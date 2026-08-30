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

	"creaves-console/locales"
	"creaves-console/models"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/mw-i18n/v2"
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

func TestE2E_V2Resync_IdempotentAndLocalized(t *testing.T) {
	tx := setupTest(t)
	rawKey, _ := seedAPIKey(t, tx, "")
	instanceID := "v2-resync-center"
	state := func(id int, species, zone, cage string, eventID string) e2ePusherEvent {
		payload, err := json.Marshal(models.EventPayload{
			Animal:        models.AnimalPayload{ID: id, Year: 2025, YearNumber: id, Species: species, Zone: zone, Cage: cage},
			CurrentStatus: "in_care",
			Translations:  map[string]map[string]string{"fr": {"species": species}, "en-US": {"species": species + " EN"}},
			StateHash:     species + "-" + zone,
		})
		require.NoError(t, err)
		return e2ePusherEvent{ID: eventID, InstanceID: instanceID, AnimalID: id, EventType: string(models.EventTypeAnimalState), Payload: payload, CreatedAt: time.Date(2025, 1, id, 10, 0, 0, 0, time.UTC)}
	}
	events := []e2ePusherEvent{
		state(1, "Hérisson", "Quarantine", "A1", "11111111-1111-4111-8111-111111111111"),
		state(2, "Renard", "Recovery", "B2", "22222222-2222-4222-8222-222222222222"),
	}
	code, response := deliverToConsole(t, tx, rawKey, events)
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 2, response["processed"])

	code, response = deliverToConsole(t, tx, rawKey, events)
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 2, response["processed"], "replayed state batch is deduplicated success")
	count, err := tx.Where("instance_id = ?", instanceID).Count(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	var first models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", instanceID, 1).First(&first))
	assert.Equal(t, 0, first.EventCount, "state snapshots do not increment lifecycle event count")
	assert.Equal(t, "Hérisson EN", first.LocalizedField("en-US", "species"))

	changed := state(1, "Hérisson", "Rehab", "", "33333333-3333-4333-8333-333333333333")
	code, response = deliverToConsole(t, tx, rawKey, []e2ePusherEvent{changed})
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 1, response["processed"])
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", instanceID, 1).First(&first))
	assert.Equal(t, "Rehab", first.Zone.String)
	assert.False(t, first.Cage.Valid, "state replacement clears omitted fields")
}

func TestE2E_V2CleanupThenResync(t *testing.T) {
	tx := setupTest(t)
	rawKey, _ := seedAPIKey(t, tx, "")
	instanceID := "v2-recovery-center"
	payload, err := json.Marshal(models.EventPayload{
		Animal:        models.AnimalPayload{ID: 9, Year: 2025, Species: "Blaireau", Zone: "Care", Cage: "C9"},
		CurrentStatus: "in_care", StateHash: "recovery-hash",
	})
	require.NoError(t, err)
	event := e2ePusherEvent{ID: "44444444-4444-4444-8444-444444444444", InstanceID: instanceID, AnimalID: 9, EventType: string(models.EventTypeAnimalState), Payload: payload, CreatedAt: time.Now().UTC()}
	code, response := deliverToConsole(t, tx, rawKey, []e2ePusherEvent{event})
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 1, response["processed"])
	require.NoError(t, purgeInstance(tx, instanceID))
	var removed models.ConsolidatedAnimal
	assert.Error(t, tx.Where("instance_id = ?", instanceID).First(&removed))

	code, response = deliverToConsole(t, tx, rawKey, []e2ePusherEvent{event})
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 1, response["processed"])
	var rebuilt models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", instanceID, 9).First(&rebuilt))
	assert.Equal(t, "Blaireau", rebuilt.Species.String)
	assert.Equal(t, "Care", rebuilt.Zone.String)
	assert.Equal(t, "C9", rebuilt.Cage.String)
	assert.Equal(t, "recovery-hash", rebuilt.StateHash.String)
	var registered models.CreavesInstance
	require.NoError(t, tx.Where("instance_id = ?", instanceID).First(&registered))
}

func TestE2E_InstanceScopedReports(t *testing.T) {
	tx := setupTest(t)
	rawKey, _ := seedAPIKey(t, tx, "")
	makeEvent := func(instance string, id int, kind string) e2ePusherEvent {
		payload, err := json.Marshal(models.EventPayload{Animal: models.AnimalPayload{ID: id, AnimalType: kind, Species: kind}, InitialStatus: "in_care"})
		require.NoError(t, err)
		return e2ePusherEvent{ID: uuid.Must(uuid.NewV4()).String(), InstanceID: instance, AnimalID: id, EventType: string(models.EventTypeAnimalDiscovered), Payload: payload, CreatedAt: time.Now().UTC()}
	}
	code, _ := deliverToConsole(t, tx, rawKey, []e2ePusherEvent{makeEvent("report-a", 1, "Mammal"), makeEvent("report-b", 2, "Bird")})
	require.Equal(t, http.StatusOK, code)
	app := newReportScopeTestApp(tx)
	app.GET("/reports/by_type/{instance_id}", ReportsByType)
	for instance, labels := range map[string][2]string{"report-a": {"Mammal", "Bird"}, "report-b": {"Bird", "Mammal"}} {
		included, excluded := labels[0], labels[1]
		req := httptest.NewRequest(http.MethodGet, "/reports/by_type/"+instance, nil)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), included)
		assert.NotContains(t, rec.Body.String(), excluded, "scoped report must not include other instance")
	}
}

func TestE2E_V2StateIdempotentAndLocalized(t *testing.T) {
	tx := setupTest(t)
	rawKey, _ := seedAPIKey(t, tx, "")
	instanceID, animalID := "v2-center", 77
	payload, err := json.Marshal(models.EventPayload{
		Animal:        models.AnimalPayload{Species: "Hérisson", Zone: "Zone 2", Cage: "A1"},
		CurrentStatus: "in_care",
		Translations:  map[string]map[string]string{"en-US": {"species": "Hedgehog"}},
		StateHash:     "state-hash-1",
	})
	require.NoError(t, err)
	eventID := uuid.Must(uuid.NewV4()).String()
	event := e2ePusherEvent{ID: eventID, InstanceID: instanceID, AnimalID: animalID, EventType: string(models.EventTypeAnimalState), Payload: payload, CreatedAt: time.Now()}
	code, response := deliverToConsole(t, tx, rawKey, []e2ePusherEvent{event})
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 1, response["processed"])
	var animal models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", instanceID, animalID).First(&animal))
	assert.Equal(t, "Hedgehog", animal.LocalizedField("en-US", "species"))
	assert.Equal(t, 0, animal.EventCount)
	assert.Equal(t, "state-hash-1", animal.StateHash.String)
	code, response = deliverToConsole(t, tx, rawKey, []e2ePusherEvent{event})
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 1, response["processed"])
	count, err := tx.Where("instance_id = ? AND animal_id = ?", instanceID, animalID).Count(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
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
			CreatedAt:  time.Now(),
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

// newConsolidatedAnimalViewTestApp mounts the show and drill-down pages with
// the i18n middleware (so the lang cookie drives tfield_localized) and the
// shared testDB connection, mirroring the production App() wiring for those
// routes without auth/session middleware.
func newConsolidatedAnimalViewTestApp(tx *pop.Connection) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	tr, err := i18n.New(locales.FS(), "en-US")
	if err != nil {
		panic(err)
	}
	T = tr
	app.Use(tr.Middleware())
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/consolidated_animals/{consolidated_animal_id}", ConsolidatedAnimalShow)
	app.GET("/consolidated_animals/{consolidated_animal_id}/drill_down", ConsolidatedAnimalDrillDown)
	return app
}

// TestE2E_TranslationsMapLocalized drives a full lifecycle (discovered ->
// released) whose payloads carry the translations map for every translatable
// field (species, animal_type, animal_age, entry_cause, entry_cause_detail,
// entry_cause_nature, outtake_type), then verifies:
//
//  1. the persisted consolidated_animal exposes localized values per language
//     through LocalizedField (fr translations, en-US canonical fallback);
//  2. the show and drill-down HTML pages render the localized entry_cause_*
//     values when the lang cookie selects fr.
func TestE2E_TranslationsMapLocalized(t *testing.T) {
	tx := setupTest(t)
	rawKey, _ := seedAPIKey(t, tx, "")
	instanceID, animalID := "localized-center", 4242

	fr := map[string]string{
		"species":            "Blaireau européen",
		"animal_type":        "Sauvage",
		"animal_age":         "Adulte",
		"entry_cause":        "Chute du nid",
		"entry_cause_detail": "Collision avec véhicule",
		"entry_cause_nature": "Traumatique",
		"outtake_type":       "Relâché",
	}

	discovered, err := json.Marshal(models.EventPayload{
		Animal: models.AnimalPayload{
			ID: animalID, Year: 2025, YearNumber: 3,
			Species: "Meles meles", AnimalType: "wild", AnimalAge: "adult",
		},
		Discovery: models.DiscoveryPayload{
			Location: "Forest Road", City: "Lyon", Date: "2025/04/01 09:15",
			EntryCause:       "collision",
			EntryCauseDetail: "vehicle collision",
			EntryCauseNature: "traumatic",
		},
		InitialStatus: "in_care",
		Translations:  map[string]map[string]string{"fr": fr},
		Timestamp:     "2025-04-01T09:15:00Z",
	})
	require.NoError(t, err)

	released, err := json.Marshal(models.EventPayload{
		Outtake: models.OuttakePayload{
			Date:     "2025/05/10 11:00",
			Type:     "release",
			Location: "Forest Reserve",
		},
		Translations: map[string]map[string]string{"fr": fr},
		Timestamp:    "2025-05-10T11:00:00Z",
	})
	require.NoError(t, err)

	base := time.Now()
	events := []e2ePusherEvent{
		{ID: uuid.Must(uuid.NewV4()).String(), InstanceID: instanceID, AnimalID: animalID, EventType: string(models.EventTypeAnimalDiscovered), Payload: discovered, CreatedAt: base},
		{ID: uuid.Must(uuid.NewV4()).String(), InstanceID: instanceID, AnimalID: animalID, EventType: string(models.EventTypeAnimalReleased), Payload: released, CreatedAt: base.Add(time.Hour)},
	}

	code, resp := deliverToConsole(t, tx, rawKey, events)
	require.Equal(t, http.StatusOK, code)
	assert.EqualValues(t, 2, resp["processed"])
	assert.EqualValues(t, 2, resp["total"])

	var animal models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", instanceID, animalID).First(&animal),
		"consolidated animal must exist for the delivered instance/animal")

	// LocalizedField returns the fr translation for every translatable field.
	for _, field := range []string{"species", "animal_type", "animal_age", "entry_cause", "entry_cause_detail", "entry_cause_nature", "outtake_type"} {
		assert.Equal(t, fr[field], animal.LocalizedField("fr", field), "fr translation for %s", field)
	}

	// en-US has no stored translations: LocalizedField falls back to canonical.
	assert.Equal(t, "Meles meles", animal.LocalizedField("en-US", "species"))
	assert.Equal(t, "wild", animal.LocalizedField("en-US", "animal_type"))
	assert.Equal(t, "adult", animal.LocalizedField("en-US", "animal_age"))
	assert.Equal(t, "collision", animal.LocalizedField("en-US", "entry_cause"))
	assert.Equal(t, "vehicle collision", animal.LocalizedField("en-US", "entry_cause_detail"))
	assert.Equal(t, "traumatic", animal.LocalizedField("en-US", "entry_cause_nature"))
	assert.Equal(t, "release", animal.LocalizedField("en-US", "outtake_type"))

	// Show and drill-down pages render the localized entry_cause_* values for
	// a fr-language session.
	app := newConsolidatedAnimalViewTestApp(tx)
	paths := []string{
		"/consolidated_animals/" + animal.ID.String(),
		"/consolidated_animals/" + animal.ID.String() + "/drill_down",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: "lang", Value: "fr"})
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "GET %s", path)
		body := rec.Body.String()
		assert.Contains(t, body, fr["entry_cause"], "%s must render localized entry_cause", path)
		assert.Contains(t, body, fr["entry_cause_detail"], "%s must render localized entry_cause_detail", path)
		assert.Contains(t, body, fr["entry_cause_nature"], "%s must render localized entry_cause_nature", path)
		assert.Contains(t, body, fr["species"], "%s must render localized species", path)
	}
}
