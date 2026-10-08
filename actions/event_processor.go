package actions

import (
	"database/sql"
	"creaves-console/models"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gobuffalo/nulls"
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

	if processedCount > 0 {
		dataCacheInvalidateAll()
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
	dataCacheInvalidateAll()

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

// eventEventTime derives the LOGICAL event time used for the per-animal
// ordering guard (BUG-8): the producer's payload "timestamp" (RFC3339) when
// parseable, else the event row's created_at — which the wire carries as the
// PRODUCER's event creation time, so both candidates are producer-side
// moments and mutually comparable. A force resync re-queues an event with a
// freshly rebuilt payload (timestamp = re-queue time) while keeping the
// original created_at; preferring the payload timestamp then correctly ranks
// the re-queued snapshot as the newest state of that animal.
func eventEventTime(payload *models.EventPayload, event *models.EventStream) time.Time {
	if payload != nil && payload.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, payload.Timestamp); err == nil && !t.IsZero() {
			return t
		}
	}
	return event.CreatedAt
}

func (ep *EventProcessor) processEvent(event *models.EventStream) error {
	payload, err := event.GetPayload()
	if err != nil {
		return err
	}

	// Destroyed animals are deletions, not states (bugs.md "Delete show as
	// deceased in console"): an explicit animal_deleted event, or any event
	// whose outtake carries the producer's error flag (the destroy flow
	// attaches the error outtake type), removes the animal from the
	// consolidated view. The row AND the animal's event history are deleted:
	// keeping the events would make the event-log checksum include the animal
	// forever and the redelivery recovery path (webhook.go existing()) would
	// resurrect the row on the next resync.
	if event.EventType == models.EventTypeAnimalDeleted || payload.Outtake.Error {
		return ep.processDeletedAnimal(event, &payload)
	}

	// Per-animal ordering guard (BUG-8): a full redelivery arrives in
	// arbitrary batch order, so a years-old historical event can be applied
	// AFTER the newest state and clobber it (died 5,775 → 2,152 in the
	// observed scramble). Application is therefore order-insensitive: an
	// event STRICTLY OLDER than the newest event time already applied to the
	// animal is recorded as processed but NOT applied. Equal timestamps
	// apply — live events are emitted with current timestamps and must never
	// be dropped, and same-moment producer events are applied last-write-wins
	// as before.
	//
	// The "newest applied event time" is persisted on the consolidated row's
	// last_state_at column (previously only stamped on state events with the
	// receipt time — the wire created_at is producer-side too, so old values
	// stay comparable; rows without a value, e.g. from before this column
	// carried event times, simply keep the old apply-everything behavior).
	//
	// Checksum bookkeeping stays intact for skipped events: the event row
	// remains in the log (marked processed), and the event-log checksum
	// fingerprints the LATEST state hash per animal by created_at, which is
	// exactly the applied one (producer created_at and payload timestamps are
	// monotone together; a force re-queue rewrites the payload to the current
	// state hash, so every re-queued snapshot of an animal carries the same
	// hash regardless of which one applies).
	eventTime := eventEventTime(&payload, event)

	// Content-addressed state events are no-ops when the latest snapshot already
	// has the same producer-supplied hash, even when delivered under a new UUID.
	consolidated, isNew, err := ep.findOrCreateConsolidatedAnimal(event.InstanceID, event.AnimalID)
	if err != nil {
		return err
	}

	if isNew {
		// No consolidated row: this is either a first-ever event or a late
		// arrival for a DESTROYED animal. A tombstone newer than the event
		// means the latter: drop the event instead of resurrecting the row.
		// The event row is removed (not kept as processed) so the animal
		// leaves no trace — mirroring what processing the delete itself did
		// to its history — and the sync bookkeeping stays identical no matter
		// where in the redelivery order the delete happened to be processed.
		tomb, err := models.FindTombstone(ep.tx, event.InstanceID, event.AnimalID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) && !models.IsMissingTableErr(err) {
			return errors.WithStack(err)
		}
		if tomb != nil && eventTime.Before(tomb.EventAt) {
			return ep.dropEventRow(event)
		}
	} else if consolidated.LastStateAt.Valid && eventTime.Before(consolidated.LastStateAt.Time) {
		// Stale event for a living animal: keep the newer applied state,
		// mark the event processed (event_streams bookkeeping stays intact).
		return ep.markEventProcessed(event)
	}

	if event.EventType != models.EventTypeAnimalState || payload.StateHash == "" ||
		!consolidated.StateHash.Valid || consolidated.StateHash.String != payload.StateHash {
		if err := consolidated.ApplyEvent(*event); err != nil {
			return err
		}
	}
	// Stamp the ordering-guard column with THIS event's time: it is the
	// newest applied event of any type (state and live transitions alike —
	// a late historical release must not beat an applied death snapshot).
	consolidated.LastStateAt = nulls.NewTime(eventTime)

	if err := ep.saveConsolidatedAnimal(consolidated, isNew); err != nil {
		return err
	}
	if isNew {
		// The animal (re-)established itself with an event newer than the
		// recorded deletion: it lives again on the producer, so the
		// tombstone must no longer guard older-than-delete replays.
		if err := ep.tx.RawQuery(
			"DELETE FROM consolidated_animal_tombstones WHERE instance_id = ? AND animal_id = ?",
			event.InstanceID, event.AnimalID,
		).Exec(); err != nil && !models.IsMissingTableErr(err) {
			return errors.WithStack(err)
		}
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

// markEventProcessed stamps an event as processed WITHOUT applying it (the
// per-animal ordering guard decided it is older than the newest state).
func (ep *EventProcessor) markEventProcessed(event *models.EventStream) error {
	now := time.Now()
	event.ProcessedAt = &now
	return errors.WithStack(ep.tx.Update(event))
}

// dropEventRow removes the event row of an event that must not leave a trace
// (late arrival for a destroyed animal whose delete already processed): the
// producer's expected set excludes destroyed animals, so keeping the row
// would skew the sync bookkeeping the same way the delete's own purge
// otherwise avoids. Nothing is applied; the caller reports the event as
// processed (the deletion semantics make it a no-op).
func (ep *EventProcessor) dropEventRow(event *models.EventStream) error {
	return errors.WithStack(ep.tx.RawQuery(
		"DELETE FROM event_streams WHERE id = ?",
		event.ID,
	).Exec())
}

// processDeletedAnimal applies an animal_deleted (or error-outtake) event —
// the producer's destroy flow. Exact semantics (bugs.md BUG-8):
//
//  1. Stale-delete rule: if the animal's consolidated row holds a NEWER
//     applied event (last_state_at after this delete's event time), the
//     delete is ancient history out-of-order — the newer state stands, the
//     row and the event log stay untouched, and the delete is just marked
//     processed. With the current producer this is unreachable (destroying
//     an animal ends its event history), but a scrambled redelivery must
//     never roll a row back to an older era on its own.
//  2. Otherwise the deletion applies: a tombstone is recorded FIRST (so a
//     crash mid-deletion still guards later stale arrivals), then the
//     consolidated row and every received event of the animal are removed —
//     including this delete event row, as before — keeping the event-log
//     checksum aligned with the producer's expected set (destroyed animals
//     are excluded there).
func (ep *EventProcessor) processDeletedAnimal(event *models.EventStream, payload *models.EventPayload) error {
	eventTime := eventEventTime(payload, event)

	consolidated := &models.ConsolidatedAnimal{}
	err := ep.tx.Where("instance_id = ? AND animal_id = ?", event.InstanceID, event.AnimalID).First(consolidated)
	if err == nil && consolidated.LastStateAt.Valid && eventTime.Before(consolidated.LastStateAt.Time) {
		// Stale delete: a newer state already applied — do not roll back.
		return ep.markEventProcessed(event)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.WithStack(err)
	}

	// Tombstone first: it guards later (re)deliveries of the animal's
	// pre-destroy history, which can arrive after this delete in any order.
	if err := ep.recordTombstone(event.InstanceID, event.AnimalID, eventTime); err != nil {
		return err
	}

	if err := ep.deleteConsolidatedAnimal(event); err != nil {
		return err
	}
	return nil
}

// recordTombstone upserts the per-animal deletion marker, keeping the NEWEST
// deletion time seen for the animal.
func (ep *EventProcessor) recordTombstone(instanceID string, animalID int, eventTime time.Time) error {
	tomb, err := models.FindTombstone(ep.tx, instanceID, animalID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !models.IsMissingTableErr(err) {
		return errors.WithStack(err)
	}
	if tomb != nil {
		if eventTime.After(tomb.EventAt) {
			tomb.EventAt = eventTime
			return errors.WithStack(ep.tx.Update(tomb))
		}
		return nil
	}
	tomb = &models.ConsolidatedAnimalTombstone{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: instanceID,
		AnimalID:   animalID,
		EventAt:    eventTime,
	}
	if err := ep.tx.Create(tomb); err != nil {
		// Migration not applied yet: run without deletion markers rather than
		// failing the delete (the guard is re-armed by the next delete once
		// `buffalo db migrate up` created the table).
		if models.IsMissingTableErr(err) {
			warnTombstonesUnavailable()
			return nil
		}
		return errors.WithStack(err)
	}
	return nil
}

// warnTombstonesUnavailable logs ONCE per process that the tombstone table is
// missing — the BUG-8 resurrection guard is inactive until the console
// migrations are applied (buffalo db migrate up).
var warnTombstonesUnavailable = sync.OnceFunc(func() {
	log.Println("WARNING: consolidated_animal_tombstones table missing — the animal_deleted " +
		"ordering guard is INACTIVE; run `buffalo db migrate up` on the console database")
})

// deleteConsolidatedAnimal removes a destroyed animal from the consolidated
// view: the consolidated row and every received event of that animal. The
// event deletion keeps the event-log checksum aligned with the producer's
// expected set (destroyed animals are excluded there) and prevents the
// redelivery recovery path from resurrecting the row.
func (ep *EventProcessor) deleteConsolidatedAnimal(event *models.EventStream) error {
	if err := ep.tx.RawQuery(
		"DELETE FROM consolidated_animals WHERE instance_id = ? AND animal_id = ?",
		event.InstanceID, event.AnimalID,
	).Exec(); err != nil {
		return errors.WithStack(err)
	}
	if err := ep.tx.RawQuery(
		"DELETE FROM event_streams WHERE instance_id = ? AND animal_id = ?",
		event.InstanceID, event.AnimalID,
	).Exec(); err != nil {
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
