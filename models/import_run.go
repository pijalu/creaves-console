package models

import (
	"encoding/json"
	"time"

	"github.com/gobuffalo/pop/v6"
	"github.com/gobuffalo/validate/v3"
	"github.com/gofrs/uuid"
)

// ImportRun tracks each import execution for monitoring and debugging
type ImportRun struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	StartedAt       time.Time  `json:"started_at" db:"started_at"`
	CompletedAt     *time.Time `json:"completed_at" db:"completed_at"`
	SourceCount     int        `json:"source_count" db:"source_count"`
	EventsImported  int        `json:"events_imported" db:"events_imported"`
	EventsProcessed int        `json:"events_processed" db:"events_processed"`
	Status          string     `json:"status" db:"status"`
	ErrorMessage    *string    `json:"error_message" db:"error_message"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

func (i ImportRun) String() string {
	ji, _ := json.Marshal(i)
	return string(ji)
}

type ImportRuns []ImportRun

func (i ImportRuns) String() string {
	ji, _ := json.Marshal(i)
	return string(ji)
}

func (i *ImportRun) Validate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

func (i *ImportRun) ValidateCreate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

func (i *ImportRun) ValidateUpdate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

// MarkRunning sets the status to running
func (i *ImportRun) MarkRunning() {
	i.Status = "running"
	i.StartedAt = time.Now()
}

// MarkCompleted sets the status to completed
func (i *ImportRun) MarkCompleted(eventsImported, eventsProcessed int) {
	now := time.Now()
	i.CompletedAt = &now
	i.Status = "completed"
	i.EventsImported = eventsImported
	i.EventsProcessed = eventsProcessed
}

// MarkFailed sets the status to failed
func (i *ImportRun) MarkFailed(err error) {
	now := time.Now()
	i.CompletedAt = &now
	i.Status = "failed"
	errMsg := err.Error()
	i.ErrorMessage = &errMsg
}
