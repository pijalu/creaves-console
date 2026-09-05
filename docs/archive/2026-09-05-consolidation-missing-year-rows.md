# Consolidation produced no rows for year>=2024 events (bugs.md item 2)

**Date:** 2026-09-05
**Status:** Implemented

## Observed

`consolidated_animals` had rows only for 2021–2023 while 5327 received events
(1618 of them for year-2024 animals) were all marked processed. 2301 distinct
event animals had processed events but no consolidated row. The table grew
only through new live deliveries, never for the affected animals.

## Root cause

Not in the consolidation logic itself — `EventProcessor.processEvent`,
`findOrCreateConsolidatedAnimal` and `ConsolidatedAnimal.ApplyEvent/applyState`
handle `payload.animal.year = 2024` correctly
(`TestEventProcessor_AnimalStateYear2024CreatesRow` passes and is kept as a
regression probe for the processor path).

The damage came from the **deletion paths**:

- `SyncManagementDeleteAllAnimals` / `SyncManagementDeleteInstanceAnimals`
  (`actions/sync_management.go`) deleted `consolidated_animals` rows but kept
  `event_streams` rows with `processed_at` set.
- `ProcessUnprocessedEvents` only replays events with `processed_at IS NULL`,
  and webhook redelivery is deduped by event UUID
  (`actions/webhook.go` idempotency check), so a kept event whose consolidated
  row was deleted could never be processed again → permanent orphan.
- The webhook receive path *does* have a disaster-recovery branch that
  re-applies a redelivered event when its consolidated row is missing, but it
  only helps for events the producer actually resends; events the console
  acknowledged as processed are not redelivered by a resync, so most orphans
  stayed orphans.

`purgeInstance` (events + rows together) and `archiveAndDeleteEvents` (events
only, archived first) are consistent in their own directions and were not
changed.

## Fix

1. Failing tests first (`actions/consolidation_repair_test.go`):
   - `TestEventProcessor_AnimalStateYear2024CreatesRow` — processor-path probe
     (passed immediately, ruling the processor out as root cause).
   - `TestSyncManagementDeleteAll_ResetsProcessedEventsForRebuild` and
     `TestSyncManagementDeleteInstance_ResetsProcessedEventsForRebuild` — RED
     before the fix: the delete handlers left `processed_at` set, so the
     runner could not rebuild the rows.
2. `SyncManagementDeleteAllAnimals` now resets `processed_at` for all kept
   events after deleting the rows; `SyncManagementDeleteInstanceAnimals` does
   it for the instance's events only. The next consolidation run replays the
   re-queued events and rebuilds exactly the deleted rows.
3. Repair on the dev DB: `buffalo t consolidation:rebuild`
   (`ProcessAllEvents` — truncate + reset + full replay).

## Issue found during the rebuild (fixed per guideline 2)

17 events failed processing with `Error 1406: Data too long for column
'intake_wounds'` — real payloads carry up to several hundred characters while
`intake_general/wounds/parasites/remarks` were `varchar(255)`.
Migration `20260905010000_widen_intake_text_columns` widens those four
free-text columns to `text` (nullable). After the migration the full replay
processed all 5327 events.

## Validation

- `SELECT year, COUNT(*) FROM consolidated_animals GROUP BY year` ==
  distribution of distinct animals in `event_streams`:
  2021=1131, 2022=1199, 2023=1379, 2024=1618; total 5327 == 5327 distinct
  event animals; 0 unprocessed events.
- Browser (agent-browser on dev server):
  `/consolidated_animals?year=2024` lists 2024 animals;
  `/sync_management` shows Stored 5327 / Expected 5327 with a 2024 cohort of
  1618; the detail page of a rebuilt 2024 animal (Canard colvert, VE15)
  shows populated species/discovery/intake/outcome fields.
- `go vet`, `staticcheck` clean; `gocognit`/`gocyclo` findings limited to
  pre-existing code untouched by this change (`models/models.go`,
  `actions/dashboard.go`, `actions/sync_checksum.go`,
  `actions/animal_sort_test.go`); `go test -count=1 -race -cover ./...` and
  `CGO_ENABLED=1 go test -tags sqlite ./...` green.
