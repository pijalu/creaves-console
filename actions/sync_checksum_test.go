//go:build sqlite
// +build sqlite

package actions

import (
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"time"

	"creaves-console/models"
	"creaves-console/templates"

	"github.com/gobuffalo/nulls"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stateHashPayload builds an animal_state payload JSON with the given hash.
func stateHashPayload(t *testing.T, hash string) []byte {
	t.Helper()
	return []byte(`{"animal":{"id":1},"state_hash":"` + hash + `"}`)
}

// seedSyncChecksumFixtures builds, for center-a:
//   - animal 1: state event (hash h1) + consolidated row with hash h1 → confirmed
//   - animal 2: state event (hash h2) + consolidated row with a STALE hash → unconfirmed (mismatch)
//   - animal 3: only a discovered event (no state event, no row) → unconfirmed (missing)
//   - animal 4: consolidated row with NO state hash → ignored for confirmation
//   - animal 5: state event with an OLDER created_at + newer hash (latest wins)
// center-b gets one unrelated event (scoping check).
func seedSyncChecksumFixtures(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.RawQuery("DELETE FROM consolidated_animals").Exec())
	require.NoError(t, testDB.RawQuery("DELETE FROM event_streams").Exec())
	require.NoError(t, testDB.RawQuery("DELETE FROM creaves_instances").Exec())

	now := time.Now().UTC()
	inst := &models.CreavesInstance{ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", Name: "center-a", FirstSeenAt: now, LastSeenAt: now}
	require.NoError(t, testDB.Create(inst))

	stateEvent := func(animalID int, hash string, at time.Time) {
		e := &models.EventStream{
			ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: animalID,
			EventType: models.EventTypeAnimalState, Payload: stateHashPayload(t, hash),
			ImportedAt: at, CreatedAt: at,
		}
		require.NoError(t, testDB.Create(e))
	}

	stateEvent(1, "h1", now.Add(-3*time.Hour))
	stateEvent(2, "h2", now.Add(-3*time.Hour))
	stateEvent(5, "h5-old", now.Add(-5*time.Hour))
	stateEvent(5, "h5-new", now.Add(-1*time.Hour))
	require.NoError(t, testDB.Create(&models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: 3,
		EventType: models.EventTypeAnimalDiscovered, Payload: []byte(`{}`),
		ImportedAt: now, CreatedAt: now,
	}))
	require.NoError(t, testDB.Create(&models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-b", AnimalID: 99,
		EventType: models.EventTypeAnimalDiscovered, Payload: []byte(`{}`),
		ImportedAt: now, CreatedAt: now,
	}))

	row := func(animalID, year int, hash string) {
		a := &models.ConsolidatedAnimal{
			ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a",
			AnimalID: animalID, Year: year, YearNumber: 1,
			Species: nulls.NewString("Hérisson"),
		}
		if hash != "" {
			a.StateHash = nulls.NewString(hash)
		}
		require.NoError(t, testDB.Create(a))
	}
	row(1, 2025, "h1")        // confirmed
	row(2, 2026, "h2-stale")  // mismatch
	row(4, 2024, "")          // no state hash
	row(5, 2026, "h5-new")    // confirmed via latest event
}

func TestStateSetChecksum_GoldenAndDeterminism(t *testing.T) {
	// Empty input: SHA-256 of empty string.
	assert.Equal(t,
		"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		StateSetChecksum(nil))

	lines := []string{"2|aaa", "1|bbb", "10|ccc"}
	got := StateSetChecksum(lines)
	// Sorted lexicographically: "1|bbb", "10|ccc", "2|aaa".
	assert.Equal(t, got, StateSetChecksum([]string{"1|bbb", "10|ccc", "2|aaa"}))
	assert.True(t, strings.HasPrefix(got, "sha256:"), "checksum must be prefixed: %s", got)
	assert.NotEqual(t, got, StateSetChecksum([]string{"2|aaa", "1|bbb"}), "different set must differ")
}

func TestInstanceSyncStatus_CountsAndChecksums(t *testing.T) {
	seedSyncChecksumFixtures(t)

	status, err := ComputeInstanceSyncStatus(testDB, "center-a")
	require.NoError(t, err)

	// Expected = distinct animals seen in ANY event for the instance: 1,2,3,5.
	assert.Equal(t, 4, status.ExpectedTotal)
	// Confirmed: animal 1 (h1==h1) and animal 5 (latest event h5-new == row).
	assert.Equal(t, 2, status.Confirmed)
	// Unconfirmed: animal 2 (stale row hash) and animal 3 (no row at all).
	assert.Equal(t, 2, status.Unconfirmed)

	// Event-log checksum covers the latest state hash of animals 1,2,5.
	assert.Equal(t,
		StateSetChecksum([]string{"1|h1", "2|h2", "5|h5-new"}),
		status.EventLogChecksum)
	// Consolidated checksum covers rows with a state hash: 1,2,5.
	assert.Equal(t,
		StateSetChecksum([]string{"1|h1", "2|h2-stale", "5|h5-new"}),
		status.ConsolidatedChecksum)
	assert.NotEqual(t, status.EventLogChecksum, status.ConsolidatedChecksum,
		"stale row must make the two checksums differ")
}

func TestInstanceSyncStatus_EmptyInstance(t *testing.T) {
	seedSyncChecksumFixtures(t)

	status, err := ComputeInstanceSyncStatus(testDB, "missing")
	require.NoError(t, err)
	assert.Equal(t, 0, status.ExpectedTotal)
	assert.Equal(t, 0, status.Confirmed)
	assert.Equal(t, 0, status.Unconfirmed)
	assert.Equal(t, StateSetChecksum(nil), status.EventLogChecksum)
	assert.Equal(t, StateSetChecksum(nil), status.ConsolidatedChecksum)
}

func TestSyncManagementIndex_ShowsSyncColumns(t *testing.T) {
	seedSyncChecksumFixtures(t)
	app := newSyncManagementTestApp(testDB, true)

	rec := perform(app, http.MethodGet, "/sync_management")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Contains(t, body, "center-a")
	// Expected/confirmed/unconfirmed numbers for center-a must be rendered.
	assert.Contains(t, body, ">4</", "expected total must render")
	assert.Contains(t, body, ">2</", "confirmed and unconfirmed counts must render")
	// Checksums must be shown (truncated) for cross-app comparison.
	assert.Contains(t, body, "sha256:", "checksum must render")
}

func TestSyncManagementIndex_YearBreakdown(t *testing.T) {
	seedSyncChecksumFixtures(t)
	app := newSyncManagementTestApp(testDB, true)

	rec := perform(app, http.MethodGet, "/sync_management")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	// Per-year stored counts must be visible so a missing current year
	// ("no animals of 2026") is immediately detectable.
	assert.Contains(t, body, "2026")
	assert.Contains(t, body, "2025")
	assert.Contains(t, body, "2024")
}

func TestSyncManagementLocaleVariantsShowSyncColumns(t *testing.T) {
	for _, locale := range []string{"", ".fr", ".de", ".nl"} {
		path := "sync_management/index.plush" + locale + ".html"
		body, err := fs.ReadFile(templates.FS(), path)
		require.NoError(t, err, "locale template %s must be embedded", path)
		assert.Contains(t, string(body), "row.Status.ExpectedTotal", "%s must render expected count", path)
		assert.Contains(t, string(body), "row.Status.ChecksumsMatch()", "%s must render checksum badge", path)
		assert.Contains(t, string(body), "yearRows", "%s must render per-year breakdown", path)
	}
}
