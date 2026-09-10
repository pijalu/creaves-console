package actions

import (
	"database/sql"
	"creaves-console/models"
	"strings"
	"time"

	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
)

// EventProcessor handles processing events into the consolidated view
type EventProcessor struct {
	tx *pop.Connection
}

// NewEventProcessor creates a new event processor
func NewEventProcessor(tx *pop.Connection) *EventProcessor {
	return &EventProcessor{tx: tx}
}

// ProcessUnprocessedEvents processes all unprocessed events in order.
// Chunked keyset replay: fetches replayBatchSize events per round instead of
// the whole backlog in one query (payload rows are large; a full resync
// backlog must not be materialized in memory). The cursor advances past
// every fetched row — poison events stay unprocessed but are stepped over,
// so they cannot loop the scan forever.
func (ep *EventProcessor) ProcessUnprocessedEvents() (int, error) {
	const replayBatchSize = 500

	processedCount := 0
	var skipped []string
	var firstErr error

	cursorTime := time.Time{}
	cursorID := uuid.Nil

	for {
		events := &models.EventStreams{}
		q := ep.tx.Where("processed_at IS NULL")
		if !cursorTime.IsZero() {
			q = q.Where("(created_at > ? OR (created_at = ? AND id > ?))", cursorTime, cursorTime, cursorID)
		}
		if err := q.Order("created_at asc, id asc").Limit(replayBatchSize).All(events); err != nil {
			return processedCount, errors.WithStack(err)
		}
		if len(*events) == 0 {
			break
		}

		for _, event := range *events {
			cursorTime = event.CreatedAt
			cursorID = event.ID
			if err := ep.processEvent(&event); err != nil {
				// Poison event: do not abort — a single malformed event must not
				// block the replay of newer events (it stays unprocessed and is
				// reported in the returned error).
				skipped = append(skipped, event.ID.String())
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			processedCount++
		}

		if len(*events) < replayBatchSize {
			break
		}
	}

	if firstErr != nil {
		return processedCount, errors.Wrapf(firstErr,
			"failed to process event %s (skipped %d unprocessable event(s): %s)",
			skipped[0], len(skipped), strings.Join(skipped, ", "))
	}

	return processedCount, nil
}

// ProcessAllEvents reprocesses all events (for rebuilding)
func (ep *EventProcessor) ProcessAllEvents() (int, error) {
	if err := ep.tx.RawQuery("DELETE FROM consolidated_animals").Exec(); err != nil {
		return 0, errors.WithStack(err)
	}
	// Rebuild wipes consolidated rows: cached dropdown values may no longer
	// exist. Drop the register reference cache before reprocessing.
	refCacheInvalidateAll()

	if err := ep.tx.RawQuery("UPDATE event_streams SET processed_at = NULL").Exec(); err != nil {
		return 0, errors.WithStack(err)
	}

	return ep.ProcessUnprocessedEvents()
}

// ProcessEventsBatch processes events in batches
func (ep *EventProcessor) ProcessEventsBatch(limit int) (int, bool, error) {
	events := &models.EventStreams{}

	if err := ep.tx.Where("processed_at IS NULL").Order("created_at asc").Limit(limit).All(events); err != nil {
		return 0, false, errors.WithStack(err)
	}

	if len(*events) == 0 {
		return 0, true, nil
	}

	processedCount := 0
	for _, event := range *events {
		if err := ep.processEvent(&event); err != nil {
			return processedCount, false, errors.Wrapf(err, "failed to process event %s", event.ID)
		}
		processedCount++
	}

	remaining, err := ep.tx.Where("processed_at IS NULL").Count(&models.EventStream{})
	if err != nil {
		return processedCount, false, errors.WithStack(err)
	}

	return processedCount, remaining == 0, nil
}

func (ep *EventProcessor) processEvent(event *models.EventStream) error {
	consolidated, isNew, err := ep.findOrCreateConsolidatedAnimal(event.InstanceID, event.AnimalID)
	if err != nil {
		return err
	}

	payload, err := event.GetPayload()
	if err != nil {
		return err
	}
	// Content-addressed state events are no-ops when the latest snapshot already
	// has the same producer-supplied hash, even when delivered under a new UUID.
	if event.EventType != models.EventTypeAnimalState || payload.StateHash == "" ||
		!consolidated.StateHash.Valid || consolidated.StateHash.String != payload.StateHash {
		if err := consolidated.ApplyEvent(*event); err != nil {
			return err
		}
	}

	if err := ep.saveConsolidatedAnimal(consolidated, isNew); err != nil {
		return err
	}
	// Change-driven cache invalidation: report the reference values this
	// event carries; unknown values drop their cached dropdown entries
	// (refcache.go).
	refCacheObservePayload(payload)

	now := time.Now()
	event.ProcessedAt = &now
	if err := ep.tx.Update(event); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

// findOrCreateConsolidatedAnimal loads the consolidated row for a source
// animal in a single query; no row means the caller must create a fresh one
// (flagged via isNew so saveConsolidatedAnimal needs no extra existence
// round trip).
func (ep *EventProcessor) findOrCreateConsolidatedAnimal(instanceID string, animalID int) (*models.ConsolidatedAnimal, bool, error) {
	consolidated := &models.ConsolidatedAnimal{}

	err := ep.tx.Where("instance_id = ? AND animal_id = ?", instanceID, animalID).First(consolidated)
	if err == nil {
		return consolidated, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, errors.WithStack(err)
	}

	consolidated = &models.ConsolidatedAnimal{
		ID:            uuid.Must(uuid.NewV4()),
		InstanceID:    instanceID,
		AnimalID:      animalID,
		CurrentStatus: "unknown",
		LastEventAt:   time.Now(),
		EventCount:    0,
	}

	return consolidated, true, nil
}

func (ep *EventProcessor) saveConsolidatedAnimal(consolidated *models.ConsolidatedAnimal, isNew bool) error {
	if isNew {
		return ep.tx.Create(consolidated)
	}
	return ep.tx.Update(consolidated)
}

// GetConsolidatedStats returns statistics
func (ep *EventProcessor) GetConsolidatedStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	count, err := ep.tx.Count(&models.ConsolidatedAnimal{})
	if err != nil {
		return nil, errors.WithStack(err)
	}
	stats["total_animals"] = count

	statusCounts := []struct {
		Status string `db:"current_status"`
		Count  int    `db:"count"`
	}{}

	if err := ep.tx.RawQuery("SELECT current_status, COUNT(*) as count FROM consolidated_animals GROUP BY current_status").All(&statusCounts); err != nil {
		return nil, errors.WithStack(err)
	}

	statusMap := make(map[string]int)
	for _, sc := range statusCounts {
		statusMap[sc.Status] = sc.Count
	}
	stats["by_status"] = statusMap

	instanceCounts := []struct {
		InstanceID string `db:"instance_id"`
		Count      int    `db:"count"`
	}{}

	if err := ep.tx.RawQuery("SELECT instance_id, COUNT(*) as count FROM consolidated_animals GROUP BY instance_id").All(&instanceCounts); err != nil {
		return nil, errors.WithStack(err)
	}

	instanceMap := make(map[string]int)
	for _, ic := range instanceCounts {
		instanceMap[ic.InstanceID] = ic.Count
	}
	stats["by_instance"] = instanceMap

	unprocessedCount, err := ep.tx.Where("processed_at IS NULL").Count(&models.EventStream{})
	if err != nil {
		return nil, errors.WithStack(err)
	}
	stats["unprocessed_events"] = unprocessedCount

	return stats, nil
}
