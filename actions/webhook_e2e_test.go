//go:build sqlite
// +build sqlite

package actions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"creaves-console/models"

	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file contains the end-to-end integration test for event forwarding.
//
// creaves and creaves-console are independent Go modules that communicate
// exclusively over HTTP. The creaves pusher (actions/webhook_pusher.go,
// deliverBatch) serializes each undelivered EventStream row into the JSON
// shape below and POSTs it to the console's /webhook/events endpoint:
//
//	{
//	  "events": [
//	    {
//	      "id":          "<uuid>",
//	      "instance_id": "<string>",
//	      "animal_id":   <int>,
//	      "event_type":  "<string>",
//	      "payload":     <json>,
//	      "created_at":  "<RFC3339>"
//	    }, ...
//	  ]
//	}
//
// Because the modules cannot import each other, this test reconstructs that
// exact wire payload (same field names, nesting and types the pusher emits) and
// drives it through the REAL receiver stack: an httptest server running the
// production WebhookEventsHandler -> EventProcessor -> DB. It then asserts the
// consolidated_animal reflects the full event sequence. This proves the
// pusher->webhook->processor->consolidated_animal contract holds end to end.

// e2ePusherEvent mirrors the creaves pusher's per-event wire object verbatim.
// (See creaves/actions/webhook_pusher.go deliverBatch().)
type e2ePusherEvent struct {
	ID         string          `json:"id"`
	InstanceID string          `json:"instance_id"`
	AnimalID   int             `json:"animal_id"`
	EventType  string          `json:"event_type"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
}

// deliverToConsole simulates the creaves pusher delivering a batch of events to
// the console webhook receiver. It serializes the events exactly as deliverBatch
// does (the {"events":[...]} envelope) and POSTs them with the Bearer key the
// pusher attaches, against the REAL WebhookEventsHandler.
func deliverToConsole(t *testing.T, tx *pop.Connection, rawKey string, events []e2ePusherEvent) (int, map[string]interface{}) {
	t.Helper()
	app := newWebhookTestApp(tx)

	payload, err := json.Marshal(map[string]interface{}{"events": events})
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/webhook/events", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return rec.Code, resp
}

// TestE2E_EventDelivery validates the complete forwarding pipeline:
//
//	creaves pusher payload  ->  WebhookEventsHandler  ->  EventProcessor
//	 -> consolidated_animals
//
// A realistic multi-event lifecycle (discovered -> status change -> released)
// is delivered in the exact shape the pusher emits and the resulting
// consolidated animal is verified.
func TestE2E_EventDelivery(t *testing.T) {
	tx := setupTest(t)
	rawKey, _ := seedAPIKey(t, tx, "")

	instanceID := "creaves-prod-01"
	animalID := 303

	// Build the event lifecycle the pusher would forward. Each payload is the
	// structured EventPayload JSON the producer writes into event_streams.
	discovered, err := json.Marshal(models.EventPayload{
		Animal:        models.AnimalPayload{Year: 2024, YearNumber: 7, Species: "Red Fox", Gender: "Male"},
		Discovery:     models.DiscoveryPayload{Location: "Forest Road", City: "Lyon", Date: "2024/01/15 10:30"},
		InitialStatus: "in_care",
		Timestamp:     "2024-01-15T10:30:00Z",
	})
	require.NoError(t, err)

	statusChange, err := json.Marshal(models.EventPayload{
		PreviousStatus: "in_care",
		CurrentStatus:  "under_treatment",
		Timestamp:      "2024-02-01T09:00:00Z",
	})
	require.NoError(t, err)

	released, err := json.Marshal(models.EventPayload{
		Outtake: models.OuttakePayload{
			Date:     "2024/03/20 14:00",
			Type:     "Released to Wild",
			Location: "Forest Reserve",
		},
		Timestamp: "2024-03-20T14:00:00Z",
	})
	require.NoError(t, err)

	base := time.Now()
	events := []e2ePusherEvent{
		{ID: uuid.Must(uuid.NewV4()).String(), InstanceID: instanceID, AnimalID: animalID, EventType: string(models.EventTypeAnimalDiscovered), Payload: discovered, CreatedAt: base},
		{ID: uuid.Must(uuid.NewV4()).String(), InstanceID: instanceID, AnimalID: animalID, EventType: string(models.EventTypeAnimalStatusChanged), Payload: statusChange, CreatedAt: base.Add(time.Hour)},
		{ID: uuid.Must(uuid.NewV4()).String(), InstanceID: instanceID, AnimalID: animalID, EventType: string(models.EventTypeAnimalReleased), Payload: released, CreatedAt: base.Add(2 * time.Hour)},
	}

	// --- pusher -> webhook receiver ---
	code, resp := deliverToConsole(t, tx, rawKey, events)
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 3, resp["processed"])
	assert.EqualValues(t, 3, resp["total"])
	_, hasErrors := resp["errors"]
	assert.False(t, hasErrors, "no errors expected, got %v", resp["errors"])

	// --- webhook -> processor -> event_streams (idempotency + processed_at) ---
	eventCount, err := tx.Where("instance_id = ?", instanceID).Count(&models.EventStream{})
	require.NoError(t, err)
	assert.Equal(t, len(events), eventCount, "all events should be persisted")

	unprocessed, err := tx.Where("processed_at IS NULL").Count(&models.EventStream{})
	require.NoError(t, err)
	assert.Equal(t, 0, unprocessed, "all events should be processed")

	// --- processor -> consolidated_animal ---
	var animal models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", instanceID, animalID).First(&animal),
		"consolidated animal must exist for the delivered instance/animal")

	assert.Equal(t, "released", animal.CurrentStatus, "final status should reflect the release event")
	assert.Equal(t, len(events), animal.EventCount, "event count should match delivered events")
	assert.Equal(t, instanceID, animal.InstanceID)
	assert.Equal(t, animalID, animal.AnimalID)

	// Identification carried through from the discovery payload.
	assert.True(t, animal.Species.Valid)
	assert.Equal(t, "Red Fox", animal.Species.String)
	assert.Equal(t, 2024, animal.Year)
	assert.Equal(t, 7, animal.YearNumber)

	// Outtake carried through from the release payload.
	assert.True(t, animal.OuttakeType.Valid)
	assert.Equal(t, "Released to Wild", animal.OuttakeType.String)
	assert.True(t, animal.OuttakeLocation.Valid)
	assert.Equal(t, "Forest Reserve", animal.OuttakeLocation.String)
}

// TestE2E_IdempotentRedelivery proves a duplicate pusher delivery (e.g. a retry
// after a transient receiver error that the pusher already persisted) does not
// corrupt the consolidated view.
func TestE2E_IdempotentRedelivery(t *testing.T) {
	tx := setupTest(t)
	rawKey, _ := seedAPIKey(t, tx, "")

	payload, err := json.Marshal(models.EventPayload{
		Animal:        models.AnimalPayload{Species: "Hedgehog"},
		InitialStatus: "in_care",
		Timestamp:     "2024-01-01T00:00:00Z",
	})
	require.NoError(t, err)

	events := []e2ePusherEvent{
		{ID: uuid.Must(uuid.NewV4()).String(), InstanceID: "inst-1", AnimalID: 5, EventType: string(models.EventTypeAnimalDiscovered), Payload: payload, CreatedAt: time.Now()},
	}

	// First delivery.
	code, resp := deliverToConsole(t, tx, rawKey, events)
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 1, resp["processed"])

	// Duplicate delivery (same event IDs, simulating a pusher retry).
	code, resp = deliverToConsole(t, tx, rawKey, events)
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 1, resp["processed"], "duplicate counts as processed but is not re-applied")

	// No duplicate rows; consolidated state intact.
	eventCount, err := tx.Count(&models.EventStream{})
	require.NoError(t, err)
	assert.Equal(t, 1, eventCount)

	animalCount, err := tx.Count(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.Equal(t, 1, animalCount)

	var animal models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", "inst-1", 5).First(&animal))
	assert.Equal(t, 1, animal.EventCount)
	assert.Equal(t, "in_care", animal.CurrentStatus)
}

// TestE2E_MultipleInstancesAndAnimals delivers a mixed batch (as a pusher would
// in a single batch) spanning two instances and verifies each consolidates
// independently.
func TestE2E_MultipleInstancesAndAnimals(t *testing.T) {
	tx := setupTest(t)
	rawKey, _ := seedAPIKey(t, tx, "")

	mk := func(instance string, animal int, species string) e2ePusherEvent {
		b, _ := json.Marshal(models.EventPayload{
			Animal:        models.AnimalPayload{Species: species},
			InitialStatus: "in_care",
			Timestamp:     "2024-01-01T00:00:00Z",
		})
		return e2ePusherEvent{
			ID:         uuid.Must(uuid.NewV4()).String(),
			InstanceID: instance,
			AnimalID:   animal,
			EventType:  string(models.EventTypeAnimalDiscovered),
			Payload:    b,
			CreatedAt:   time.Now(),
		}
	}

	events := []e2ePusherEvent{
		mk("inst-a", 1, "Fox"),
		mk("inst-a", 2, "Owl"),
		mk("inst-b", 1, "Bat"),
	}

	code, resp := deliverToConsole(t, tx, rawKey, events)
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 3, resp["processed"])

	total, err := tx.Count(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.Equal(t, 3, total)

	instA, err := tx.Where("instance_id = ?", "inst-a").Count(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.Equal(t, 2, instA)

	instB, err := tx.Where("instance_id = ?", "inst-b").Count(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.Equal(t, 1, instB)
}
