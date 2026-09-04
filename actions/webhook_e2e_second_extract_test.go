//go:build sqlite
// +build sqlite

package actions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"creaves-console/models"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_SecondFullExtractKeepsAllYears reproduces the "no animals of 2026
// after full extract" incident end-to-end and proves it cannot recur:
//
//  1. A first full extract (animal_state events for 2024/2025/2026) is
//     delivered through the real webhook handler and consolidated.
//  2. The console side purges the instance (cleanup) and the producer
//     re-delivers the same full state set ("second full extract"), this time
//     with one malformed legacy ("poison") event older than everything else.
//  3. The rebuild (ProcessUnprocessedEvents) must skip the poison event
//     without blocking the tail — every year, including the current one,
//     must end up consolidated, and the instance sync status must show
//     confirmed=all with matching checksums.
func TestE2E_SecondFullExtractKeepsAllYears(t *testing.T) {
	tx := setupTest(t)
	rawKey, _ := seedAPIKey(t, tx, "")

	instanceID := "center-e2e-extract"
	type animalSpec struct {
		id        int
		year      int
		number    int
		species   string
		stateHash string
	}
	animals := []animalSpec{
		{401, 2024, 11, "Red Fox", "hash-401"},
		{402, 2025, 12, "Hedgehog", "hash-402"},
		{403, 2026, 13, "Wild Boar", "hash-403"}, // current year — the lost one
	}

	stateEvent := func(t *testing.T, spec animalSpec, at time.Time) e2ePusherEvent {
		t.Helper()
		payload, err := json.Marshal(models.EventPayload{
			Animal: models.AnimalPayload{
				ID: spec.id, Year: spec.year, YearNumber: spec.number, Species: spec.species,
			},
			CurrentStatus: "in_care",
			StateHash:     spec.stateHash,
		})
		require.NoError(t, err)
		return e2ePusherEvent{
			ID: uuid.Must(uuid.NewV4()).String(), InstanceID: instanceID, AnimalID: spec.id,
			EventType: string(models.EventTypeAnimalState), Payload: payload, CreatedAt: at,
		}
	}

	base := time.Now().Add(-48 * time.Hour)
	firstExtract := make([]e2ePusherEvent, 0, len(animals))
	for i, spec := range animals {
		firstExtract = append(firstExtract, stateEvent(t, spec, base.Add(time.Duration(i)*time.Minute)))
	}

	// --- First full extract: all years consolidated ---
	code, resp := deliverToConsole(t, tx, rawKey, firstExtract)
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, len(animals), resp["processed"])

	for _, spec := range animals {
		count, err := tx.Where("instance_id = ? AND animal_id = ? AND year = ?", instanceID, spec.id, spec.year).
			Count(&models.ConsolidatedAnimal{})
		require.NoError(t, err)
		assert.Equal(t, 1, count, "year %d must be consolidated after the first extract", spec.year)
	}

	// --- Console cleanup purge (instance cleanup deletes received events and
	// the consolidated rows) ---
	require.NoError(t, tx.RawQuery("DELETE FROM consolidated_animals WHERE instance_id = ?", instanceID).Exec())
	require.NoError(t, tx.RawQuery("DELETE FROM event_streams WHERE instance_id = ?", instanceID).Exec())

	// --- Second full extract: same state set re-delivered, plus one poison
	// event OLDER than everything (the incident trigger shape). The poison
	// payload is valid JSON on the wire (the pusher only emits marshaled
	// payloads) but unprocessable by the processor.
	poisonCreated := base.Add(-2 * time.Hour)
	poisonPayload := json.RawMessage(`12345`)
	secondExtract := []e2ePusherEvent{{
		ID: uuid.Must(uuid.NewV4()).String(), InstanceID: instanceID, AnimalID: 999,
		EventType: string(models.EventTypeAnimalState), Payload: poisonPayload, CreatedAt: poisonCreated,
	}}
	for i, spec := range animals {
		secondExtract = append(secondExtract, stateEvent(t, spec, base.Add(time.Duration(i)*time.Minute)))
	}
	code, resp = deliverToConsole(t, tx, rawKey, secondExtract)
	require.Equal(t, http.StatusOK, code)
	// The poison event is reported but must not block the rest.
	assert.EqualValues(t, len(animals), resp["processed"])
	assert.EqualValues(t, len(animals)+1, resp["total"])

	// --- Rebuild: replay of everything still unprocessed (the poison event) ---
	// The healthy state events were already applied synchronously by the
	// handler; only the poison event remains pending. The rebuild must skip
	// it, report it, and leave the consolidated view untouched.
	processed, err := NewEventProcessor(tx).ProcessUnprocessedEvents()
	require.Error(t, err, "the poison event must be reported")
	assert.Contains(t, err.Error(), "skipped")
	assert.Equal(t, 0, processed, "no healthy event may remain pending after the handler's synchronous processing")

	for _, spec := range animals {
		count, err := tx.Where("instance_id = ? AND animal_id = ? AND year = ?", instanceID, spec.id, spec.year).
			Count(&models.ConsolidatedAnimal{})
		require.NoError(t, err)
		assert.Equal(t, 1, count, "year %d must survive the second full extract", spec.year)
	}

	// Per-year visibility on the consolidated view: the 2026 bucket exists.
	var y2026 int
	require.NoError(t, tx.RawQuery("SELECT COUNT(*) FROM consolidated_animals WHERE instance_id = ? AND year = 2026", instanceID).First(&y2026))
	assert.Equal(t, 1, y2026, "no animals of 2026 after full extract — incident regression")

	// Poison stays unprocessed for later inspection.
	var unprocessed int
	require.NoError(t, tx.RawQuery("SELECT COUNT(*) FROM event_streams WHERE instance_id = ? AND processed_at IS NULL", instanceID).First(&unprocessed))
	assert.Equal(t, 1, unprocessed)

	// --- Instance sync status: verified sync across all years ---
	status, err := ComputeInstanceSyncStatus(tx, instanceID)
	require.NoError(t, err)
	// The poison event counts as an expected-but-unconfirmable animal (999):
	// exactly the kind of discrepancy the sync-management view must surface.
	assert.Equal(t, len(animals)+1, status.ExpectedTotal)
	assert.Equal(t, len(animals), status.Confirmed, "every delivered state must be confirmed on the consolidated rows")
	assert.Equal(t, 1, status.Unconfirmed, "only the poison phantom animal stays unconfirmed")
	assert.True(t, status.ChecksumsMatch(), "event-log and consolidated checksums must match (got %s vs %s)", status.EventLogChecksum, status.ConsolidatedChecksum)

	// And the checksum equals the independent fingerprint over the delivered
	// state set — the value the producer would show as its expected checksum.
	lines := make([]string, 0, len(animals))
	for _, spec := range animals {
		lines = append(lines, fmt.Sprintf("%d|%s", spec.id, spec.stateHash))
	}
	assert.Equal(t, StateSetChecksum(lines), status.EventLogChecksum)
}
