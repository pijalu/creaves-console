package actions

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"creaves-console/models"
)

func TestRefCacheValuesCachedPerScope(t *testing.T) {
	refCacheReset()
	builds := 0
	build := func() ([]string, error) {
		builds++
		return []string{"A", "B"}, nil
	}

	v1, err := refCacheGetValues("species", "", build)
	require.NoError(t, err)
	v2, err := refCacheGetValues("species", "", build)
	require.NoError(t, err)
	require.Equal(t, []string{"A", "B"}, v1)
	require.Same(t, &v1[0], &v2[0], "second call must return the cached slice")
	require.Equal(t, 1, builds, "builder must run once for a warm scope")

	// A different scope builds (and caches) independently.
	_, err = refCacheGetValues("species", "inst-1", build)
	require.NoError(t, err)
	require.Equal(t, 2, builds)

	// A different field builds independently too.
	_, err = refCacheGetValues("animal_age", "", build)
	require.NoError(t, err)
	require.Equal(t, 3, builds)
}

func TestRefCacheBuildErrorNotCached(t *testing.T) {
	refCacheReset()
	var builds int32
	build := func() ([]string, error) {
		n := atomic.AddInt32(&builds, 1)
		if n < 2 {
			return nil, errors.New("db down")
		}
		return []string{"Recovered"}, nil
	}

	_, err := refCacheGetValues("species", "", build)
	require.Error(t, err)

	// The failure must not poison the cache: the retry rebuilds.
	v, err := refCacheGetValues("species", "", build)
	require.NoError(t, err)
	require.Equal(t, []string{"Recovered"}, v)
}

func TestRefCacheLabelsCachedAndErrorNotCached(t *testing.T) {
	refCacheReset()
	builds := 0
	build := func() (map[string]string, error) {
		builds++
		if builds == 1 {
			return nil, errors.New("db down")
		}
		return map[string]string{"Hérisson": "Hedgehog"}, nil
	}

	_, err := refCacheGetLabels("species", "", "en-US", build)
	require.Error(t, err)

	// The failure must not poison the cache: the retry rebuilds.
	m, err := refCacheGetLabels("species", "", "en-US", build)
	require.NoError(t, err)
	require.Equal(t, "Hedgehog", m["Hérisson"])

	// Another language is an independent cache entry and builds fresh.
	m2, err := refCacheGetLabels("species", "", "fr", build)
	require.NoError(t, err)
	require.Equal(t, "Hedgehog", m2["Hérisson"])
	require.Equal(t, 3, builds)
}

func TestRefCacheObserveInvalidatesUnknownValuesOnly(t *testing.T) {
	refCacheReset()
	speciesBuilds := 0
	speciesBuild := func() ([]string, error) {
		speciesBuilds++
		return []string{"Hérisson", "Chouette"}, nil
	}
	cityBuilds := 0
	cityBuild := func() ([]string, error) {
		cityBuilds++
		return []string{"Strasbourg"}, nil
	}
	_, err := refCacheGetValues("species", "", speciesBuild)
	require.NoError(t, err)
	require.Equal(t, 1, speciesBuilds)

	// Known values keep the cache warm.
	refCacheObservePayload(models.EventPayload{Animal: models.AnimalPayload{Species: "Chouette"}})
	_, err = refCacheGetValues("species", "", speciesBuild)
	require.NoError(t, err)
	require.Equal(t, 1, speciesBuilds)

	// An unknown value invalidates the field entry.
	refCacheObservePayload(models.EventPayload{Animal: models.AnimalPayload{Species: "Loir gris"}})
	_, err = refCacheGetValues("species", "", speciesBuild)
	require.NoError(t, err)
	require.Equal(t, 2, speciesBuilds, "cache must rebuild after an unknown value")

	// Observing an unknown city must not touch species; city itself was
	// cold (never built), so the observation is a no-op there.
	refCacheObservePayload(models.EventPayload{Discovery: models.DiscoveryPayload{City: "Atlantis"}})
	_, err = refCacheGetValues("species", "", speciesBuild)
	require.NoError(t, err)
	require.Equal(t, 2, speciesBuilds)
	_, err = refCacheGetValues("discovery_city", "", cityBuild)
	require.NoError(t, err)
	require.Equal(t, 1, cityBuilds)
}

func TestRefCacheInvalidateAllAndReset(t *testing.T) {
	refCacheReset()
	builds := 0
	build := func() ([]string, error) {
		builds++
		return []string{"A"}, nil
	}
	_, err := refCacheGetValues("species", "", build)
	require.NoError(t, err)

	refCacheInvalidateAll()
	_, err = refCacheGetValues("species", "", build)
	require.NoError(t, err)
	require.Equal(t, 2, builds)
}

func TestRefCacheSingleFlight(t *testing.T) {
	refCacheReset()
	var builds int32
	build := func() ([]string, error) {
		atomic.AddInt32(&builds, 1)
		time.Sleep(50 * time.Millisecond)
		return []string{"A"}, nil
	}

	const goroutines = 10
	var wg sync.WaitGroup
	results := make([][]string, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := refCacheGetValues("species", "", build)
			if err == nil {
				results[i] = v
			}
		}(i)
	}
	wg.Wait()

	require.Equal(t, int32(1), atomic.LoadInt32(&builds),
		"concurrent cold reads must share one rebuild")
	for _, v := range results {
		require.Equal(t, []string{"A"}, v)
	}
}

func TestRefCacheConcurrentObserveAndInvalidate(t *testing.T) {
	// Race-detector target: readers, observers and invalidators on one field.
	refCacheReset()
	build := func() ([]string, error) {
		return []string{"Known"}, nil
	}
	_, err := refCacheGetValues("species", "", build)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				switch i % 3 {
				case 0:
					_, _ = refCacheGetValues("species", "", build)
				case 1:
					refCacheObservePayload(models.EventPayload{Animal: models.AnimalPayload{Species: "Known"}})
				default:
					refCacheObservePayload(models.EventPayload{Animal: models.AnimalPayload{Species: "Mystery"}})
				}
			}
		}(i)
	}
	wg.Wait()
}
