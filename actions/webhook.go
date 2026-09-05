package actions

import (
	"bytes"
	"creaves-console/models"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
)

// WebhookEvent represents a single event in the webhook payload
type WebhookEvent struct {
	ID         string          `json:"id"`
	InstanceID string          `json:"instance_id"`
	AnimalID   int             `json:"animal_id"`
	EventType  string          `json:"event_type"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
}

// InstanceInfo identifies the producing Creaves installation.
type InstanceInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SyncAnnouncement is the producer-announced expected sync state attached
// to resync delivery envelopes: the console stores it on the instance row
// and displays stored/announced(expected) with a checksum comparison, so
// undelivered animals are detectable (they never arrive as events).
type SyncAnnouncement struct {
	ExpectedTotal    int        `json:"expected_total"`
	ExpectedChecksum string     `json:"expected_checksum"`
	AnnouncedAt      *time.Time `json:"announced_at"`
}

// WebhookConfirmation tells the producer, per processed animal_state event,
// the state hash the console actually stored. The producer marks those
// events acknowledged — its "Delivered & current" count is fed by these
// confirmations, not by bare HTTP acceptance.
type WebhookConfirmation struct {
	ID        string `json:"id"`
	StateHash string `json:"state_hash"`
}

// WebhookPayload represents the incoming webhook request body.
type WebhookPayload struct {
	ContractVersion int               `json:"contract_version,omitempty"`
	Instance        *InstanceInfo     `json:"instance,omitempty"`
	Sync            *SyncAnnouncement `json:"sync,omitempty"`
	Events          []WebhookEvent    `json:"events"`
}

// WebhookEventsHandler receives events from Creaves instances
func WebhookEventsHandler(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return c.Render(http.StatusInternalServerError, r.JSON(map[string]string{"error": "no transaction found"}))
	}

	// Authenticate the request
	authHeader := c.Request().Header.Get("Authorization")
	if authHeader == "" {
		return c.Render(http.StatusUnauthorized, r.JSON(map[string]string{"error": "missing authorization header"}))
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return c.Render(http.StatusUnauthorized, r.JSON(map[string]string{"error": "invalid authorization format"}))
	}

	rawKey := parts[1]

	// Find and validate the API key
	key, err := findAndAuthenticateKey(tx, rawKey)
	if err != nil {
		return c.Render(http.StatusUnauthorized, r.JSON(map[string]string{"error": "invalid api key"}))
	}

	// Update last used time
	now := time.Now()
	key.LastUsedAt = &now
	if err := tx.Update(key); err != nil {
		// Log but don't fail the request
		fmt.Printf("Failed to update last_used_at for key %s: %v\n", key.ID, err)
	}

	// Parse request body and enforce batch caps (see parseWebhookEnvelope).
	payload, errStatus, errMsg := parseWebhookEnvelope(c)
	if errStatus != 0 {
		return c.Render(errStatus, r.JSON(map[string]string{"error": errMsg}))
	}

	// Register envelope instance before ingest; restricted keys define the
	// canonical instance identifier, regardless of client casing.
	if err := registerEnvelopeInstance(tx, key, payload, now); err != nil {
		return c.Render(http.StatusForbidden, r.JSON(map[string]string{"error": "instance_id mismatch"}))
	}

	// Store the producer's announced expected sync state (resync envelopes
	// only). The announcement is auxiliary: a failure to persist it must
	// never reject the batch.
	storeAnnouncedSyncFromEnvelope(tx, key, payload, now)

	if len(payload.Events) == 0 {
		return c.Render(http.StatusOK, r.JSON(map[string]interface{}{
			"processed": 0,
			"message":   "no events to process",
		}))
	}

	// Process events. Per-event ingestion lives in the webhookIngest helper
	// so this handler stays readable.
	ing := &webhookIngest{tx: tx, processor: NewEventProcessor(tx), key: key, envelope: payload, now: now,
		confirmations: []WebhookConfirmation{}, processedIDs: []string{}}
	for i := range payload.Events {
		ing.event(&payload.Events[i])
	}

	response := map[string]interface{}{
		"processed":     ing.processedCount,
		"total":         len(payload.Events),
		"processed_ids": ing.processedIDs,
		"confirmed":     ing.confirmations,
	}

	if len(ing.eventErrors) > 0 {
		response["errors"] = ing.eventErrors
	}

	return c.Render(http.StatusOK, r.JSON(response))
}

// webhookIngest carries the per-batch ingestion state for one webhook
// request. Per processed animal_state event the console records a
// confirmation (the state hash it stored): the producer's resync page
// reports "Delivered & current" from these acknowledgements. Events that
// could not be processed are absent from confirmations, so the producer
// keeps them unconfirmed and retries.
type webhookIngest struct {
	tx             *pop.Connection
	processor      *EventProcessor
	key            *models.WebhookAPIKey
	envelope       *WebhookPayload
	now            time.Time
	processedCount int
	processedIDs   []string
	confirmations  []WebhookConfirmation
	eventErrors    []string
}

// event ingests one webhook event: instance checks, dedupe by deterministic
// ID, synchronous processing and acknowledgement.
func (w *webhookIngest) event(webhookEvent *WebhookEvent) {
	// Restricted keys define authoritative routing; comparisons are
	// case-insensitive while persisted identifiers remain canonical.
	eventInstanceID := webhookEvent.InstanceID
	if w.envelope.Instance != nil && w.envelope.Instance.ID != "" && !strings.EqualFold(eventInstanceID, w.envelope.Instance.ID) {
		w.eventErrors = append(w.eventErrors, fmt.Sprintf("event %s: instance block mismatch", webhookEvent.ID))
		return
	}
	if w.key.InstanceID != "" {
		if !strings.EqualFold(eventInstanceID, w.key.InstanceID) {
			w.eventErrors = append(w.eventErrors, fmt.Sprintf("event %s: instance_id mismatch", webhookEvent.ID))
			return
		}
		eventInstanceID = w.key.InstanceID
	}
	// Lazily register v1 event sources and maintain latest event timestamp.
	eventAt := webhookEvent.CreatedAt
	if err := models.UpsertByInstanceID(w.tx, eventInstanceID, "", "", w.now, &eventAt); err != nil {
		w.eventErrors = append(w.eventErrors, fmt.Sprintf("event %s: instance registration failed", webhookEvent.ID))
		return
	}

	// Parse event ID
	eventID, err := uuid.FromString(webhookEvent.ID)
	if err != nil {
		w.eventErrors = append(w.eventErrors, fmt.Sprintf("invalid event id: %s", webhookEvent.ID))
		return
	}

	// Check if event already exists (idempotent)
	exists, err := w.tx.Where("id = ?", eventID).Exists(&models.EventStream{})
	if err != nil {
		w.eventErrors = append(w.eventErrors, fmt.Sprintf("failed to check event %s: %v", webhookEvent.ID, err))
		return
	}
	if exists {
		w.existing(eventID, webhookEvent)
		return
	}
	w.fresh(eventID, eventInstanceID, webhookEvent)
}

// existing re-ingests a redelivered event: self-healing reprocess, legacy
// payload refresh and acknowledgement.
func (w *webhookIngest) existing(eventID uuid.UUID, webhookEvent *WebhookEvent) {
	// The event was already received. If it was not processed yet
	// (e.g. a previous delivery created the row but processing
	// failed), process it now so a redelivery is self-healing.
	existing := &models.EventStream{}
	if err := w.tx.Find(existing, eventID); err != nil {
		w.eventErrors = append(w.eventErrors, fmt.Sprintf("failed to load existing event %s: %v", webhookEvent.ID, err))
		return
	}
	needsProcessing := existing.ProcessedAt == nil
	if !needsProcessing {
		// Disaster-recovery path: if the consolidated row was lost
		// (e.g. console DB partially wiped) but the event log
		// survived, a resync redelivers the same deterministic
		// event UUIDs. Re-apply them so the center's state is
		// rebuilt instead of skipped as "already processed".
		rowExists, err := w.tx.Where("instance_id = ? AND animal_id = ?", existing.InstanceID, existing.AnimalID).Exists(&models.ConsolidatedAnimal{})
		if err != nil {
			w.eventErrors = append(w.eventErrors, fmt.Sprintf("failed to check consolidated row for event %s: %v", webhookEvent.ID, err))
			return
		}
		needsProcessing = !rowExists
	}
	// A redelivery can carry a refreshed payload (e.g. the producer's
	// force resync backfills payload.state_hash onto legacy events so
	// they become acknowledgeable). Adopt it and re-apply so the
	// consolidated snapshot — including its state_hash — matches the
	// event log; processEvent is idempotent for unchanged state.
	if !bytes.Equal(existing.Payload, webhookEvent.Payload) {
		existing.Payload = webhookEvent.Payload
		if err := w.tx.Update(existing); err != nil {
			w.eventErrors = append(w.eventErrors, fmt.Sprintf("failed to refresh payload for event %s: %v", webhookEvent.ID, err))
			return
		}
		needsProcessing = true
	}
	if needsProcessing {
		if err := w.processor.processEvent(existing); err != nil {
			w.eventErrors = append(w.eventErrors, fmt.Sprintf("failed to process existing event %s: %v", webhookEvent.ID, err))
			return
		}
	}
	w.confirmStateHash(existing, webhookEvent.ID)
	w.processedIDs = append(w.processedIDs, webhookEvent.ID)
	w.processedCount++
}

// fresh creates and synchronously processes a first-time event.
func (w *webhookIngest) fresh(eventID uuid.UUID, eventInstanceID string, webhookEvent *WebhookEvent) {
	event := &models.EventStream{
		ID:         eventID,
		InstanceID: eventInstanceID,
		AnimalID:   webhookEvent.AnimalID,
		EventType:  models.EventType(webhookEvent.EventType),
		Payload:    webhookEvent.Payload,
		SourceDB:   "", // Deprecated, no longer used
		ImportedAt: time.Now(),
		CreatedAt:  webhookEvent.CreatedAt,
	}

	if err := w.tx.Create(event); err != nil {
		w.eventErrors = append(w.eventErrors, fmt.Sprintf("failed to create event %s: %v", webhookEvent.ID, err))
		return
	}

	// Process event immediately (synchronous)
	if err := w.processor.processEvent(event); err != nil {
		w.eventErrors = append(w.eventErrors, fmt.Sprintf("failed to process event %s: %v", webhookEvent.ID, err))
		return
	}

	w.confirmStateHash(event, webhookEvent.ID)
	w.processedIDs = append(w.processedIDs, webhookEvent.ID)
	w.processedCount++
}

// confirmStateHash appends an acknowledgement when the event is an
// animal_state event whose payload carries a non-empty state hash (the
// value the processor stored on the consolidated snapshot).
func (w *webhookIngest) confirmStateHash(event *models.EventStream, wireID string) {
	if models.EventType(event.EventType) != models.EventTypeAnimalState {
		return
	}
	p, err := event.GetPayload()
	if err != nil || p.StateHash == "" {
		return
	}
	w.confirmations = append(w.confirmations, WebhookConfirmation{ID: wireID, StateHash: p.StateHash})
}

// parseWebhookEnvelope reads and validates the request body and enforces
// the batch-size cap. On success it returns the payload with status 0; on
// failure a non-zero HTTP status and error message ready to render.
func parseWebhookEnvelope(c buffalo.Context) (*WebhookPayload, int, string) {
	// Size-capped read: a webhook batch never needs to be larger than a few
	// MB; unbounded reads are a memory-exhaustion vector.
	const maxWebhookBody = 10 << 20 // 10 MB
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxWebhookBody)
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, http.StatusRequestEntityTooLarge, "request body too large"
		}
		return nil, http.StatusBadRequest, "failed to read body"
	}

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, http.StatusBadRequest, "invalid json"
	}

	// Cap batch size to bound processing time per request.
	const maxEventsPerBatch = 1000
	if len(payload.Events) > maxEventsPerBatch {
		return nil, http.StatusRequestEntityTooLarge, fmt.Sprintf("too many events in batch (max %d)", maxEventsPerBatch)
	}
	return &payload, 0, ""
}

// registerEnvelopeInstance registers the producing instance named by the
// envelope before ingest. Restricted keys define the canonical identifier;
// a mismatch is a hard rejection.
func registerEnvelopeInstance(tx *pop.Connection, key *models.WebhookAPIKey, payload *WebhookPayload, now time.Time) error {
	if payload.Instance == nil || payload.Instance.ID == "" {
		return nil
	}
	envelopeID := payload.Instance.ID
	if key.InstanceID != "" {
		if !strings.EqualFold(envelopeID, key.InstanceID) {
			return fmt.Errorf("instance_id mismatch")
		}
		envelopeID = key.InstanceID
	}
	if err := models.UpsertByInstanceID(tx, envelopeID, payload.Instance.Name, payload.Instance.Description, now, nil); err != nil {
		fmt.Printf("Failed to upsert instance %s: %v\n", envelopeID, err)
	}
	return nil
}

// storeAnnouncedSyncFromEnvelope persists the producer's announced expected
// sync state on the instance row. Auxiliary data: a failure to persist it
// must never reject the batch.
func storeAnnouncedSyncFromEnvelope(tx *pop.Connection, key *models.WebhookAPIKey, payload *WebhookPayload, now time.Time) {
	if payload.Sync == nil || payload.Sync.ExpectedChecksum == "" {
		return
	}
	instanceID := ""
	switch {
	case key.InstanceID != "":
		instanceID = key.InstanceID
	case payload.Instance != nil:
		instanceID = payload.Instance.ID
	}
	if instanceID == "" {
		return
	}
	announcedAt := now
	if payload.Sync.AnnouncedAt != nil {
		announcedAt = *payload.Sync.AnnouncedAt
	}
	if err := models.StoreAnnouncedSyncStatus(tx, instanceID, payload.Sync.ExpectedTotal, payload.Sync.ExpectedChecksum, announcedAt); err != nil {
		fmt.Printf("Failed to store announced sync status for %s: %v\n", instanceID, err)
	}
}

// findAndAuthenticateKey looks up an API key by hash and authenticates it
func findAndAuthenticateKey(tx *pop.Connection, rawKey string) (*models.WebhookAPIKey, error) {
	keys := &models.WebhookAPIKeys{}
	if err := tx.Where("active = ?", true).All(keys); err != nil {
		return nil, err
	}

	for _, key := range *keys {
		if key.Authenticate(rawKey) {
			return &key, nil
		}
	}

	return nil, errors.New("invalid api key")
}
