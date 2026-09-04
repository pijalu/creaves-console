//go:build sqlite
// +build sqlite

package actions

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"creaves-console/models"

	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedStateEvent inserts an animal_state snapshot event for the given
// animal/year with full intake+discovery payload. When processed is true the
// event is stored as already processed (consolidation consumed it).
func seedStateEvent(t *testing.T, tx *pop.Connection, instanceID string, animalID, year int, processed bool) models.EventStream {
	t.Helper()
	payload := map[string]interface{}{
		"animal": map[string]interface{}{
			"id": animalID, "year": year, "year_number": 1,
			"species": "Hérisson", "cage": "VE12", "gender": "Femelle",
		},
		"discovery": map[string]interface{}{
			"location": "Verger", "city": "Lille", "postal_code": "59000",
			"date": "2024/05/01 10:00", "entry_cause": "Blessé",
		},
		"intake":         map[string]interface{}{"date": "2024/05/01 11:00"},
		"current_status": "in_care",
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	event := &models.EventStream{
		ID:          uuid.Must(uuid.NewV4()),
		InstanceID:  instanceID,
		AnimalID:    animalID,
		EventType:   models.EventTypeAnimalState,
		Payload:     raw,
		ImportedAt:  time.Now().UTC(),
		ProcessedAt: nil,
		CreatedAt:   time.Date(year, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	if processed {
		now := time.Now().UTC()
		event.ProcessedAt = &now
	}
	require.NoError(t, tx.Create(event))
	return *event
}

// TestEventProcessor_AnimalStateYear2024CreatesRow is the bugs.md item 2
// failing-test-first probe: a processed animal_state event whose payload
// carries animal.year=2024 must produce a consolidated_animals row with
// year 2024 (full intake + discovery applied).
func TestEventProcessor_AnimalStateYear2024CreatesRow(t *testing.T) {
	tx := setupTest(t)

	seedStateEvent(t, tx, "lagrange", 4063, 2024, false)

	count, err := NewEventProcessor(tx).ProcessUnprocessedEvents()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var row models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", "lagrange", 4063).First(&row))
	assert.Equal(t, 2024, row.Year)
	assert.Equal(t, 1, row.YearNumber)
	assert.Equal(t, "in_care", row.CurrentStatus)
	assert.Equal(t, "Hérisson", row.Species.String)
	assert.Equal(t, "VE12", row.Cage.String)
	assert.Equal(t, "Verger", row.DiscoveryLocation.String)
	assert.Equal(t, "Lille", row.DiscoveryCity.String)
}

// TestSyncManagementDeleteAll_ResetsProcessedEventsForRebuild reproduces the
// production incident behind bugs.md item 2: deleting consolidated animals
// while keeping their events marked processed strands those events forever —
// ProcessUnprocessedEvents skips them and webhook redelivery is deduped by
// event ID, so no consolidated row is ever rebuilt. The delete must reset
// processed_at so the consolidation runner replays the kept events.
func TestSyncManagementDeleteAll_ResetsProcessedEventsForRebuild(t *testing.T) {
	tx := setupTest(t)

	seedStateEvent(t, tx, "lagrange", 4063, 2024, true)

	// A consolidated row exists before the delete (processing happened).
	row := &models.ConsolidatedAnimal{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "lagrange", AnimalID: 4063,
		Year: 2024, CurrentStatus: "in_care", LastEventAt: time.Now().UTC(),
	}
	require.NoError(t, tx.Create(row))

	app := newSyncManagementTestApp(tx, true)
	rec := postForm(app, "/sync_management/delete-all-animals", url.Values{})
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, 0, countAnimals(t, tx, ""), "all animals must be deleted")

	// Events are kept but must be re-processable, otherwise the kept
	// events can never rebuild their consolidated rows.
	var processed int
	require.NoError(t, tx.RawQuery("SELECT COUNT(*) FROM event_streams WHERE processed_at IS NOT NULL").First(&processed))
	assert.Equal(t, 0, processed, "delete-all must reset processed_at so consolidation rebuilds rows")

	count, err := NewEventProcessor(tx).ProcessUnprocessedEvents()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var rebuilt models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", "lagrange", 4063).First(&rebuilt))
	assert.Equal(t, 2024, rebuilt.Year)
	assert.Equal(t, "VE12", rebuilt.Cage.String)
}

// TestSyncManagementDeleteInstance_ResetsProcessedEventsForRebuild is the
// instance-scoped variant: only the deleted instance's events are reset for
// replay, other instances keep their processed markers and rows.
func TestSyncManagementDeleteInstance_ResetsProcessedEventsForRebuild(t *testing.T) {
	tx := setupTest(t)

	seedStateEvent(t, tx, "lagrange", 4063, 2024, true)
	seedStateEvent(t, tx, "other", 7, 2023, true)

	for _, seed := range []struct {
		instance string
		animal   int
		year     int
	}{
		{"lagrange", 4063, 2024},
		{"other", 7, 2023},
	} {
		row := &models.ConsolidatedAnimal{
			ID: uuid.Must(uuid.NewV4()), InstanceID: seed.instance, AnimalID: seed.animal,
			Year: seed.year, CurrentStatus: "in_care", LastEventAt: time.Now().UTC(),
		}
		require.NoError(t, tx.Create(row))
	}

	app := newSyncManagementTestApp(tx, true)
	rec := postForm(app, "/sync_management/delete-instance-animals", url.Values{
		"instance_id": {"lagrange"},
	})
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, 0, countAnimals(t, tx, "lagrange"), "instance animals must be deleted")
	assert.Equal(t, 1, countAnimals(t, tx, "other"), "other instance animals must be kept")

	// Only the purged instance's events are reset for replay.
	var lagrangePending, otherPending int
	require.NoError(t, tx.RawQuery("SELECT COUNT(*) FROM event_streams WHERE instance_id = 'lagrange' AND processed_at IS NULL").First(&lagrangePending))
	require.NoError(t, tx.RawQuery("SELECT COUNT(*) FROM event_streams WHERE instance_id = 'other' AND processed_at IS NULL").First(&otherPending))
	assert.Equal(t, 1, lagrangePending, "deleted instance events must be re-queued")
	assert.Equal(t, 0, otherPending, "untouched instance events must stay processed")

	count, err := NewEventProcessor(tx).ProcessUnprocessedEvents()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var rebuilt models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", "lagrange", 4063).First(&rebuilt))
	assert.Equal(t, 2024, rebuilt.Year)
}
