package actions

import (
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

// WebhookPayload represents the incoming webhook request body.
type WebhookPayload struct {
	ContractVersion int            `json:"contract_version,omitempty"`
	Instance        *InstanceInfo  `json:"instance,omitempty"`
	Events          []WebhookEvent `json:"events"`
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

	// Parse request body
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.Render(http.StatusBadRequest, r.JSON(map[string]string{"error": "failed to read body"}))
	}

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return c.Render(http.StatusBadRequest, r.JSON(map[string]string{"error": "invalid json"}))
	}

	// Register envelope instance before ingest; upsert errors are non-fatal.
	if payload.Instance != nil && payload.Instance.ID != "" {
		if err := models.UpsertByInstanceID(tx, payload.Instance.ID, payload.Instance.Name, payload.Instance.Description, now, nil); err != nil {
			fmt.Printf("Failed to upsert instance %s: %v\n", payload.Instance.ID, err)
		}
	}

	if len(payload.Events) == 0 {
		return c.Render(http.StatusOK, r.JSON(map[string]interface{}{
			"processed": 0,
			"message":   "no events to process",
		}))
	}

	// Process events
	processor := NewEventProcessor(tx)
	processedCount := 0
	processedIDs := []string{}
	eventErrors := []string{}

	for _, webhookEvent := range payload.Events {
		// Validate instance_id if key is restricted
		// Envelope instance, when present, must match event source.
		if payload.Instance != nil && payload.Instance.ID != "" && webhookEvent.InstanceID != payload.Instance.ID {
			eventErrors = append(eventErrors, fmt.Sprintf("event %s: instance block mismatch", webhookEvent.ID))
			continue
		}
		// Lazily register v1 event sources and maintain latest event timestamp.
		eventAt := webhookEvent.CreatedAt
		if err := models.UpsertByInstanceID(tx, webhookEvent.InstanceID, "", "", now, &eventAt); err != nil {
			eventErrors = append(eventErrors, fmt.Sprintf("event %s: instance registration failed", webhookEvent.ID))
			continue
		}
		if key.InstanceID != "" && webhookEvent.InstanceID != key.InstanceID {
			eventErrors = append(eventErrors, fmt.Sprintf("event %s: instance_id mismatch", webhookEvent.ID))
			continue
		}

		// Parse event ID
		eventID, err := uuid.FromString(webhookEvent.ID)
		if err != nil {
			eventErrors = append(eventErrors, fmt.Sprintf("invalid event id: %s", webhookEvent.ID))
			continue
		}

		// Check if event already exists (idempotent)
		exists, err := tx.Where("id = ?", eventID).Exists(&models.EventStream{})
		if err != nil {
			eventErrors = append(eventErrors, fmt.Sprintf("failed to check event %s: %v", webhookEvent.ID, err))
			continue
		}
		if exists {
			// The event was already received. If it was not processed yet
			// (e.g. a previous delivery created the row but processing
			// failed), process it now so a redelivery is self-healing.
			existing := &models.EventStream{}
			if err := tx.Find(existing, eventID); err != nil {
				eventErrors = append(eventErrors, fmt.Sprintf("failed to load existing event %s: %v", webhookEvent.ID, err))
				continue
			}
			needsProcessing := existing.ProcessedAt == nil
			if !needsProcessing {
				// Disaster-recovery path: if the consolidated row was lost
				// (e.g. console DB partially wiped) but the event log
				// survived, a resync redelivers the same deterministic
				// event UUIDs. Re-apply them so the center's state is
				// rebuilt instead of skipped as "already processed".
				rowExists, err := tx.Where("instance_id = ? AND animal_id = ?", existing.InstanceID, existing.AnimalID).Exists(&models.ConsolidatedAnimal{})
				if err != nil {
					eventErrors = append(eventErrors, fmt.Sprintf("failed to check consolidated row for event %s: %v", webhookEvent.ID, err))
					continue
				}
				needsProcessing = !rowExists
			}
			if needsProcessing {
				if err := processor.processEvent(existing); err != nil {
					eventErrors = append(eventErrors, fmt.Sprintf("failed to process existing event %s: %v", webhookEvent.ID, err))
					continue
				}
			}
			processedIDs = append(processedIDs, webhookEvent.ID)
			processedCount++
			continue
		}

		// Create event
		event := &models.EventStream{
			ID:         eventID,
			InstanceID: webhookEvent.InstanceID,
			AnimalID:   webhookEvent.AnimalID,
			EventType:  models.EventType(webhookEvent.EventType),
			Payload:    webhookEvent.Payload,
			SourceDB:   "", // Deprecated, no longer used
			ImportedAt: time.Now(),
			CreatedAt:  webhookEvent.CreatedAt,
		}

		if err := tx.Create(event); err != nil {
			eventErrors = append(eventErrors, fmt.Sprintf("failed to create event %s: %v", webhookEvent.ID, err))
			continue
		}

		// Process event immediately (synchronous)
		if err := processor.processEvent(event); err != nil {
			eventErrors = append(eventErrors, fmt.Sprintf("failed to process event %s: %v", webhookEvent.ID, err))
			continue
		}

		processedIDs = append(processedIDs, webhookEvent.ID)
		processedCount++
	}

	response := map[string]interface{}{
		"processed":     processedCount,
		"total":         len(payload.Events),
		"processed_ids": processedIDs,
	}

	if len(eventErrors) > 0 {
		response["errors"] = eventErrors
		return c.Render(http.StatusOK, r.JSON(response))
	}

	return c.Render(http.StatusOK, r.JSON(response))
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
