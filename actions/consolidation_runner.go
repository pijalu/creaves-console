package actions

import (
	"creaves-console/models"
	"fmt"
	"time"

	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
)

// ConsolidationRunner orchestrates the processing workflow
type ConsolidationRunner struct {
	tx        *pop.Connection
	processor *EventProcessor
}

// NewConsolidationRunner creates a new runner
func NewConsolidationRunner(tx *pop.Connection) *ConsolidationRunner {
	return &ConsolidationRunner{
		tx:        tx,
		processor: NewEventProcessor(tx),
	}
}

// RunResult contains the results of a consolidation run
type RunResult struct {
	ImportRunID      uuid.UUID
	StartedAt        time.Time
	CompletedAt      time.Time
	Duration         time.Duration
	EventsProcessed  int
	Errors           []string
}

// Run executes a full process cycle
func (cr *ConsolidationRunner) Run() (*RunResult, error) {
	startTime := time.Now()
	result := &RunResult{
		StartedAt: startTime,
		Errors:    []string{},
	}

	// Create import run record
	importRun := &models.ImportRun{
		ID:        uuid.Must(uuid.NewV4()),
		Status:    "running",
		StartedAt: startTime,
	}
	if err := cr.tx.Create(importRun); err != nil {
		return nil, errors.Wrap(err, "failed to create import run record")
	}
	result.ImportRunID = importRun.ID

	// Process events
	processedCount, processErr := cr.processor.ProcessUnprocessedEvents()
	if processErr != nil {
		result.Errors = append(result.Errors, processErr.Error())
	}
	result.EventsProcessed = processedCount

	// Complete
	result.CompletedAt = time.Now()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)

	// Update import run record
	if len(result.Errors) > 0 {
		importRun.MarkFailed(fmt.Errorf("%d errors occurred", len(result.Errors)))
	} else {
		importRun.MarkCompleted(0, result.EventsProcessed)
	}
	if err := cr.tx.Update(importRun); err != nil {
		return result, errors.Wrap(err, "failed to update import run record")
	}

	return result, nil
}

// RunDryRun simulates processing without making changes
func (cr *ConsolidationRunner) RunDryRun() (*RunResult, error) {
	// TODO: Implement dry-run logic
	return nil, fmt.Errorf("dry-run not yet implemented")
}

// GetLastRun returns the most recent import run
func (cr *ConsolidationRunner) GetLastRun() (*models.ImportRun, error) {
	importRun := &models.ImportRun{}
	if err := cr.tx.Order("started_at desc").First(importRun); err != nil {
		return nil, err
	}
	return importRun, nil
}

// GetRunHistory returns recent import runs
func (cr *ConsolidationRunner) GetRunHistory(limit int) (models.ImportRuns, error) {
	runs := models.ImportRuns{}
	if err := cr.tx.Order("started_at desc").Limit(limit).All(&runs); err != nil {
		return nil, err
	}
	return runs, nil
}
