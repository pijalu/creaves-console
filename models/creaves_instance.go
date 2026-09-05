package models

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
	"github.com/gobuffalo/validate/v3"
	"github.com/gobuffalo/validate/v3/validators"
	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
)

// CreavesInstance is a registered source Creaves installation.
type CreavesInstance struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	InstanceID  string     `json:"instance_id" db:"instance_id"`
	Name        string     `json:"name" db:"name"`
	Description string     `json:"description" db:"description"`
	FirstSeenAt time.Time  `json:"first_seen_at" db:"first_seen_at"`
	LastSeenAt  time.Time  `json:"last_seen_at" db:"last_seen_at"`
	LastEventAt *time.Time `json:"last_event_at" db:"last_event_at"`
	// Producer-announced expected sync state (from the "sync" block of a
	// resync delivery envelope): what the source instance says the console
	// should hold. Displayed on /sync_management as stored/announced with a
	// checksum comparison; replaces guessing the expected set from received
	// events.
	AnnouncedExpectedTotal    nulls.Int    `json:"announced_expected_total" db:"announced_expected_total"`
	AnnouncedExpectedChecksum nulls.String `json:"announced_expected_checksum" db:"announced_expected_checksum"`
	AnnouncedAt               nulls.Time   `json:"announced_at" db:"announced_at"`
	CreatedAt                 time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt                 time.Time    `json:"updated_at" db:"updated_at"`
}

type CreavesInstances []CreavesInstance

func (i CreavesInstance) String() string {
	value, _ := json.Marshal(i)
	return string(value)
}

func (i CreavesInstances) String() string {
	value, _ := json.Marshal(i)
	return string(value)
}

func (i *CreavesInstance) Validate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.Validate(&validators.StringIsPresent{Field: i.InstanceID, Name: "InstanceID"}), nil
}

// UpsertByInstanceID registers an instance and refreshes non-empty metadata.
func UpsertByInstanceID(tx *pop.Connection, instanceID, name, description string, seenAt time.Time, eventTimes ...*time.Time) error {
	var eventAt *time.Time
	if len(eventTimes) > 0 {
		eventAt = eventTimes[0]
	}
	if instanceID == "" {
		return errors.New("instance id is required")
	}
	instance := &CreavesInstance{}
	if err := tx.Where("instance_id = ?", instanceID).First(instance); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		instance = &CreavesInstance{ID: uuid.Must(uuid.NewV4()), InstanceID: instanceID, FirstSeenAt: seenAt, LastSeenAt: seenAt}
	}
	if instance.FirstSeenAt.IsZero() {
		instance.FirstSeenAt = seenAt
	}
	if name != "" {
		instance.Name = name
	}
	if description != "" {
		instance.Description = description
	}
	if seenAt.After(instance.LastSeenAt) {
		instance.LastSeenAt = seenAt
	}
	if eventAt != nil && (instance.LastEventAt == nil || eventAt.After(*instance.LastEventAt)) {
		v := *eventAt
		instance.LastEventAt = &v
	}
	if instance.CreatedAt.IsZero() {
		return tx.Create(instance)
	}
	return tx.Update(instance)
}

// StoreAnnouncedSyncStatus persists the producer-announced expected sync
// state (from a resync delivery envelope's "sync" block) on the instance
// row. The console never derives the expected set from the events it
// happens to have received — the producer's announcement is authoritative.
func StoreAnnouncedSyncStatus(tx *pop.Connection, instanceID string, expectedTotal int, expectedChecksum string, announcedAt time.Time) error {
	if instanceID == "" {
		return errors.New("instance id is required")
	}
	if expectedChecksum == "" {
		return errors.New("announced checksum must not be empty")
	}
	instance := &CreavesInstance{}
	if err := tx.Where("instance_id = ?", instanceID).First(instance); err != nil {
		return err
	}
	instance.AnnouncedExpectedTotal = nulls.NewInt(expectedTotal)
	instance.AnnouncedExpectedChecksum = nulls.NewString(expectedChecksum)
	instance.AnnouncedAt = nulls.NewTime(announcedAt)
	return tx.Update(instance)
}

func (i *CreavesInstance) ValidateCreate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

func (i *CreavesInstance) ValidateUpdate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}
