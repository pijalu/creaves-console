package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gobuffalo/pop/v6"
	"github.com/gobuffalo/validate/v3"
	"github.com/gobuffalo/validate/v3/validators"
	"github.com/gofrs/uuid"
	"golang.org/x/crypto/bcrypt"
)

// WebhookAPIKey represents an API key for authenticating webhook requests
type WebhookAPIKey struct {
	ID        uuid.UUID `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	KeyHash   string    `json:"-" db:"key_hash"`
	KeyPrefix string    `json:"key_prefix" db:"key_prefix"`
	// KeyValue is retained so administrators can retrieve the configured key.
	// It is never serialized in API responses.
	KeyValue   string     `json:"-" db:"key_value"`
	InstanceID string     `json:"instance_id" db:"instance_id"`
	Active     bool       `json:"active" db:"active"`
	LastUsedAt *time.Time `json:"last_used_at" db:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at" db:"updated_at"`
}

// WebhookAPIKeys is a slice of WebhookAPIKey
type WebhookAPIKeys []WebhookAPIKey

// String returns the JSON representation
func (w WebhookAPIKey) String() string {
	jw, _ := json.Marshal(w)
	return string(jw)
}

// String returns the JSON representation
func (w WebhookAPIKeys) String() string {
	jw, _ := json.Marshal(w)
	return string(jw)
}

// Validate gets run every time you call a "pop.Validate*" method
func (w *WebhookAPIKey) Validate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.Validate(
		&validators.StringIsPresent{Field: w.Name, Name: "Name"},
		&validators.StringIsPresent{Field: w.KeyHash, Name: "KeyHash"},
	), nil
}

// ValidateCreate gets run every time you call "pop.ValidateAndCreate" method
func (w *WebhookAPIKey) ValidateCreate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

// ValidateUpdate gets run every time you call "pop.ValidateAndUpdate" method
func (w *WebhookAPIKey) ValidateUpdate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

// GenerateKey creates a new raw API key and returns it along with the hash and prefix
func GenerateKey() (rawKey string, hash string, prefix string, err error) {
	id, err := uuid.NewV4()
	if err != nil {
		return "", "", "", err
	}

	rawKey = fmt.Sprintf("creaves_%s", id.String())
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	if err != nil {
		return "", "", "", err
	}

	hash = string(hashBytes)
	prefix = id.String()[:8]

	return rawKey, hash, prefix, nil
}

// Authenticate checks if the provided raw key matches the stored hash
func (w *WebhookAPIKey) Authenticate(rawKey string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(w.KeyHash), []byte(rawKey))
	return err == nil
}
