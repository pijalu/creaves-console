package models

import (
	"time"

	"github.com/gobuffalo/pop/v6"
	"github.com/gobuffalo/validate/v3"
	"github.com/gobuffalo/validate/v3/validators"
	"github.com/gofrs/uuid"
)

// EventStreamArchive stores the JSONL snapshot of the events removed by one
// admin deletion run. The archive lives in the database (no files outside the
// DB) and is written in the same transaction as the deletion itself.
type EventStreamArchive struct {
	ID         uuid.UUID `json:"id" db:"id"`
	Scope      string    `json:"scope" db:"scope"`
	InstanceID string    `json:"instance_id" db:"instance_id"`
	EventCount int       `json:"event_count" db:"event_count"`
	Content    string    `json:"content" db:"content"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

// EventStreamArchives is a slice of EventStreamArchive.
type EventStreamArchives []EventStreamArchive

// Validate runs basic validations on the archive record.
func (a *EventStreamArchive) Validate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.Validate(
		&validators.StringIsPresent{Field: a.Scope, Name: "Scope"},
		&validators.IntIsGreaterThan{Field: a.EventCount, Name: "EventCount", Compared: 0},
		&validators.StringIsPresent{Field: a.Content, Name: "Content"},
	), nil
}
