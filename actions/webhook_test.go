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

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newWebhookTestApp builds a minimal Buffalo app that injects the shared
// testDB connection as the request-scoped "tx" value and mounts only the
// webhook receiver. It deliberately omits CSRF/session middleware so requests
// can be exercised through net/http/httptest.
func newWebhookTestApp(tx *pop.Connection) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.POST("/webhook/events", WebhookEventsHandler)
	return app
}

// seedAPIKey inserts an active API key into the DB and returns the raw key
// (suitable for the Authorization header) plus the stored record.
func seedAPIKey(t *testing.T, tx *pop.Connection, instanceID string) (rawKey string, stored *models.WebhookAPIKey) {
	t.Helper()
	rawKey, hash, prefix, err := models.GenerateKey()
	require.NoError(t, err)

	stored = &models.WebhookAPIKey{
		ID:         uuid.Must(uuid.NewV4()),
		Name:       "test-key",
		KeyHash:    hash,
		KeyPrefix:  prefix,
		InstanceID: instanceID,
		Active:     true,
	}
	require.NoError(t, tx.Create(stored))
	return rawKey, stored
}

// postWebhook issues an authenticated (or not) POST to the webhook receiver
// running under the provided app and returns the recorded response.
func postWebhook(app *buffalo.App, authHeader string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/webhook/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func TestWebhookEventsHandler_MissingAuth(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)

	body, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{}})
	rec := postWebhook(app, "", body)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "missing authorization header", resp["error"])
}

// TestWebhookEventsHandler_NoTransaction covers the defensive 500 path when no
// DB transaction is present on the context.
func TestWebhookEventsHandler_NoTransaction(t *testing.T) {
	setupTest(t)
	// An app that does NOT inject "tx" into the context.
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.POST("/webhook/events", WebhookEventsHandler)

	body, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{}})
	rec := postWebhook(app, "Bearer anything", body)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestWebhookEventsHandler_InvalidAuthFormat(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)

	body, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{}})
	// Not a "Bearer <token>" format.
	rec := postWebhook(app, "Token abc123", body)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "invalid authorization format", resp["error"])
}

func TestWebhookEventsHandler_InvalidAPIKey(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	seedAPIKey(t, tx, "")

	body, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{}})
	// Well-formed but unknown key.
	rec := postWebhook(app, "Bearer creaves_unknown", body)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "invalid api key", resp["error"])
}

func TestWebhookEventsHandler_EmptyEvents(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")

	body, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{}})
	rec := postWebhook(app, "Bearer "+rawKey, body)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.EqualValues(t, 0, resp["processed"])
	assert.Equal(t, "no events to process", resp["message"])
}

func TestWebhookEventsHandler_RejectsInvalidJSON(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")

	rec := postWebhook(app, "Bearer "+rawKey, []byte("{not json"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "invalid json", resp["error"])
}

func TestWebhookEventsHandler_ProcessesEvent(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")

	eventID := uuid.Must(uuid.NewV4())
	event := WebhookEvent{
		ID:         eventID.String(),
		InstanceID: "inst-1",
		AnimalID:   42,
		EventType:  string(models.EventTypeAnimalDiscovered),
		Payload:    []byte(`{"animal":{"species":"Fox"},"initial_status":"in_care"}`),
		CreatedAt:  time.Now(),
	}
	payload, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{event}})

	rec := postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.EqualValues(t, 1, resp["processed"])
	assert.EqualValues(t, 1, resp["total"])
	if errs, ok := resp["errors"]; ok {
		assert.Nil(t, errs)
	}

	// Event should be persisted.
	count, err := tx.Where("id = ?", eventID).Count(&models.EventStream{})
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Consolidated animal should have been built synchronously.
	var animal models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", "inst-1", 42).First(&animal))
	assert.Equal(t, "in_care", animal.CurrentStatus)
	assert.Equal(t, 1, animal.EventCount)
	assert.Equal(t, "Fox", animal.Species.String)

	// API key last_used_at should be updated.
	keys := &models.WebhookAPIKeys{}
	require.NoError(t, tx.All(keys))
	require.Len(t, *keys, 1)
	assert.NotNil(t, (*keys)[0].LastUsedAt)
}

func TestWebhookEventsHandler_Idempotent(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")

	eventID := uuid.Must(uuid.NewV4())
	event := WebhookEvent{
		ID:         eventID.String(),
		InstanceID: "inst-1",
		AnimalID:   7,
		EventType:  string(models.EventTypeAnimalDiscovered),
		Payload:    []byte(`{"initial_status":"in_care"}`),
		CreatedAt:  time.Now(),
	}
	payload, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{event}})

	// First delivery.
	rec := postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)

	// Second delivery of the exact same event (replay).
	rec = postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// Idempotent path counts the duplicate as processed but does not duplicate rows.
	assert.EqualValues(t, 1, resp["processed"])

	count, err := tx.Where("id = ?", eventID).Count(&models.EventStream{})
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Only one consolidated animal.
	total, err := tx.Count(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
}

func TestWebhookEventsHandler_InstanceRestriction(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	// Key restricted to inst-allowed.
	rawKey, _ := seedAPIKey(t, tx, "inst-allowed")

	event := WebhookEvent{
		ID:         uuid.Must(uuid.NewV4()).String(),
		InstanceID: "inst-other",
		AnimalID:   1,
		EventType:  string(models.EventTypeAnimalDiscovered),
		Payload:    []byte(`{"initial_status":"in_care"}`),
		CreatedAt:  time.Now(),
	}
	payload, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{event}})

	rec := postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// Mismatched event skipped: 0 processed out of 1.
	assert.EqualValues(t, 0, resp["processed"])
	assert.EqualValues(t, 1, resp["total"])
	errs, ok := resp["errors"].([]interface{})
	require.True(t, ok && len(errs) == 1, "expected one error, got %v", resp["errors"])

	// Nothing persisted.
	total, err := tx.Count(&models.EventStream{})
	require.NoError(t, err)
	assert.Equal(t, 0, total)
}

func TestWebhookEventsHandler_InvalidEventID(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")

	event := WebhookEvent{
		ID:         "not-a-uuid",
		InstanceID: "inst-1",
		AnimalID:   1,
		EventType:  string(models.EventTypeAnimalDiscovered),
		Payload:    []byte(`{"initial_status":"in_care"}`),
		CreatedAt:  time.Now(),
	}
	payload, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{event}})

	rec := postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.EqualValues(t, 0, resp["processed"])
	errs, ok := resp["errors"].([]interface{})
	require.True(t, ok && len(errs) == 1, "expected one error, got %v", resp["errors"])
}

func TestWebhookEventsHandler_BatchAndLastUsedAt(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, stored := seedAPIKey(t, tx, "")

	// Pre-set last_used_at so we can prove it advances.
	past := time.Now().Add(-24 * time.Hour)
	stored.LastUsedAt = &past
	require.NoError(t, tx.Update(stored))

	events := []WebhookEvent{
		{
			ID:         uuid.Must(uuid.NewV4()).String(),
			InstanceID: "inst-1",
			AnimalID:   1,
			EventType:  string(models.EventTypeAnimalDiscovered),
			Payload:    []byte(`{"initial_status":"in_care"}`),
			CreatedAt:  time.Now(),
		},
		{
			ID:         uuid.Must(uuid.NewV4()).String(),
			InstanceID: "inst-1",
			AnimalID:   2,
			EventType:  string(models.EventTypeAnimalDiscovered),
			Payload:    []byte(`{"initial_status":"in_care"}`),
			CreatedAt:  time.Now(),
		},
	}
	payload, _ := json.Marshal(WebhookPayload{Events: events})

	rec := postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.EqualValues(t, 2, resp["processed"])
	assert.EqualValues(t, 2, resp["total"])

	total, err := tx.Count(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.Equal(t, 2, total)

	// last_used_at advanced past the seeded value.
	keys := &models.WebhookAPIKeys{}
	require.NoError(t, tx.All(keys))
	require.Len(t, *keys, 1)
	require.NotNil(t, (*keys)[0].LastUsedAt)
	assert.True(t, (*keys)[0].LastUsedAt.After(past))
}

// TestWebhookEventsHandler_PartialFailure verifies that when a batch contains
// a mix of valid and invalid events, the receiver:
//   - processes the valid events (persisted + consolidated),
//   - reports only the valid event IDs in processed_ids,
//   - returns errors for the invalid ones without aborting the whole batch.
//
// This is the core of partial-failure handling: the pusher relies on
// processed_ids to know which events it may safely mark as delivered.
func TestWebhookEventsHandler_PartialFailure(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")

	goodID := uuid.Must(uuid.NewV4())
	goodEvent := WebhookEvent{
		ID:         goodID.String(),
		InstanceID: "inst-1",
		AnimalID:   1,
		EventType:  string(models.EventTypeAnimalDiscovered),
		Payload:    []byte(`{"initial_status":"in_care"}`),
		CreatedAt:  time.Now(),
	}
	// An invalid (non-UUID) id forces a per-event failure.
	badEvent := WebhookEvent{
		ID:         "not-a-uuid",
		InstanceID: "inst-1",
		AnimalID:   2,
		EventType:  string(models.EventTypeAnimalDiscovered),
		Payload:    []byte(`{"initial_status":"in_care"}`),
		CreatedAt:  time.Now(),
	}
	payload, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{goodEvent, badEvent}})

	rec := postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	// One processed out of two.
	assert.EqualValues(t, 1, resp["processed"])
	assert.EqualValues(t, 2, resp["total"])

	// processed_ids contains ONLY the accepted (good) event.
	processedIDs, ok := resp["processed_ids"].([]interface{})
	require.True(t, ok, "processed_ids must be present, got %v", resp["processed_ids"])
	require.Len(t, processedIDs, 1)
	assert.Equal(t, goodID.String(), processedIDs[0])

	// An error is reported for the bad event.
	errs, ok := resp["errors"].([]interface{})
	require.True(t, ok && len(errs) == 1, "expected one error, got %v", resp["errors"])

	// Only the good event was persisted.
	total, err := tx.Count(&models.EventStream{})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
}

// TestWebhookEventsHandler_RedeliveryReprocessesUnprocessed proves the
// self-healing path: if an event row exists but was never processed (e.g. a
// previous delivery created the row then crashed before processing), a
// redelivery reprocesses it instead of silently skipping it.
func TestWebhookEventsHandler_RedeliveryReprocessesUnprocessed(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")

	eventID := uuid.Must(uuid.NewV4())
	// Insert an event row that exists but is NOT processed (processed_at NULL),
	// simulating a prior delivery that failed during processing.
	existing := &models.EventStream{
		ID:         eventID,
		InstanceID: "inst-1",
		AnimalID:   99,
		EventType:  models.EventTypeAnimalDiscovered,
		Payload:    []byte(`{"initial_status":"in_care"}`),
		ImportedAt: time.Now(),
		CreatedAt:  time.Now(),
	}
	require.NoError(t, tx.Create(existing))

	// Sanity: no consolidated animal yet.
	total, err := tx.Count(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.Equal(t, 0, total)

	// Redeliver the same event.
	event := WebhookEvent{
		ID:         eventID.String(),
		InstanceID: "inst-1",
		AnimalID:   99,
		EventType:  string(models.EventTypeAnimalDiscovered),
		Payload:    []byte(`{"initial_status":"in_care"}`),
		CreatedAt:  time.Now(),
	}
	payload, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{event}})
	rec := postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.EqualValues(t, 1, resp["processed"])
	processedIDs, ok := resp["processed_ids"].([]interface{})
	require.True(t, ok)
	require.Len(t, processedIDs, 1)

	// The redelivery processed it: consolidated animal now exists and the
	// event is marked processed.
	total, err = tx.Count(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.Equal(t, 1, total)

	var got models.EventStream
	require.NoError(t, tx.Find(&got, eventID))
	assert.NotNil(t, got.ProcessedAt, "redelivery should have processed the existing event")
}

func TestWebhookEventsHandler_AutoRegistersUnknownInstance(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")
	created := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	event := WebhookEvent{ID: uuid.Must(uuid.NewV4()).String(), InstanceID: "center-new", AnimalID: 9, EventType: string(models.EventTypeAnimalDiscovered), Payload: []byte(`{"initial_status":"in_care"}`), CreatedAt: created}
	body, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{event}})
	rec := postWebhook(app, "Bearer "+rawKey, body)
	require.Equal(t, http.StatusOK, rec.Code)
	var instances models.CreavesInstances
	require.NoError(t, tx.All(&instances))
	require.Len(t, instances, 1)
	assert.Equal(t, "center-new", instances[0].InstanceID)
	assert.WithinDuration(t, instances[0].FirstSeenAt, instances[0].LastSeenAt, time.Second)
	require.NotNil(t, instances[0].LastEventAt)
	assert.WithinDuration(t, created, *instances[0].LastEventAt, time.Second)
}

func TestWebhookEventsHandler_InstanceBlockUpserts(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")
	makeBody := func(name string) []byte {
		e := WebhookEvent{ID: uuid.Must(uuid.NewV4()).String(), InstanceID: "center-block", AnimalID: 1, EventType: string(models.EventTypeAnimalDiscovered), Payload: []byte(`{"initial_status":"in_care"}`), CreatedAt: time.Now()}
		b, _ := json.Marshal(map[string]interface{}{"contract_version": 2, "instance": map[string]string{"id": "center-block", "name": name}, "events": []WebhookEvent{e}})
		return b
	}
	require.Equal(t, http.StatusOK, postWebhook(app, "Bearer "+rawKey, makeBody("First Name")).Code)
	require.Equal(t, http.StatusOK, postWebhook(app, "Bearer "+rawKey, makeBody("Updated Name")).Code)
	var instances models.CreavesInstances
	require.NoError(t, tx.All(&instances))
	require.Len(t, instances, 1)
	assert.Equal(t, "Updated Name", instances[0].Name)
}

func TestWebhookEventsHandler_InstanceBlockMismatchFailsEvent(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")
	e := WebhookEvent{ID: uuid.Must(uuid.NewV4()).String(), InstanceID: "event-instance", AnimalID: 1, EventType: string(models.EventTypeAnimalDiscovered), Payload: []byte(`{"initial_status":"in_care"}`), CreatedAt: time.Now()}
	body, _ := json.Marshal(map[string]interface{}{"contract_version": 2, "instance": map[string]string{"id": "other-instance"}, "events": []WebhookEvent{e}})
	rec := postWebhook(app, "Bearer "+rawKey, body)
	var response map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.EqualValues(t, 0, response["processed"])
	assert.NotEmpty(t, response["errors"])
	count, err := tx.Count(&models.EventStream{})
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
