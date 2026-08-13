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

// WebhookPayload represents the incoming webhook request body
type WebhookPayload struct {
	Events []WebhookEvent `json:"events"`
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

	if len(payload.Events) == 0 {
		return c.Render(http.StatusOK, r.JSON(map[string]interface{}{
			"processed": 0,
			"message":   "no events to process",
		}))
	}

	// Process events
	processor := NewEventProcessor(tx)
	processedCount := 0
	eventErrors := []string{}

	for _, webhookEvent := range payload.Events {
		// Validate instance_id if key is restricted
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
			processedCount++
			continue // Already processed, count as success
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

		processedCount++
	}

	response := map[string]interface{}{
		"processed": processedCount,
		"total":     len(payload.Events),
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
