# Plan: Event cleanup controls — processed-events scope

**Date:** 2026-09-11
**Status:** Implemented, tested, closed
**Bugs.md item:** "creaves-console: event cleanup controls — Admin must have
ways to delete or clean up events globally, processed events, or events
belonging to a specific instance."

## Goal

The admin event deletion UI (`/events/delete`, built 2026-09-04) already
covered global (`all`) and per-instance (`instance`) scopes. The remaining
gap was a **processed events** scope: purge events already applied to the
consolidated view while keeping unprocessed ones queued for processing.

## Design

| Scope | Meaning | Typed confirmation |
|-------|---------|--------------------|
| `all` | every row in `event_streams` | exact string `DELETE ALL` |
| `processed` | rows with `processed_at IS NOT NULL` | exact string `DELETE PROCESSED` |
| `instance` | rows with the given `instance_id` | exact `instance_id` |

Anything else → `422`, nothing deleted, no archive written.

- The processed scope reuses the existing archive-then-delete flow:
  keyset-batched JSONL archiving into `event_stream_archives`, deletion of
  exactly those rows, one database transaction.
- Form validation and scope→WHERE mapping factored into
  `validateEventsDeleteForm` / `applyEventsScope` helpers, which lowered
  `archiveAndDeleteEvents` gocognit 34→30 / gocyclo 16→14 and removed
  `EventsDeleteCreate` from the gocyclo>12 list (below pre-change levels).
- Delete form template (EN/FR/DE/NL) gained the new scope option and the
  extended confirmation hint.

## Issues found during testing (fixed, guideline 2/3)

- **pop double-commit noise** (pre-existing pattern, confirmed in dev log
  during E2E): `archiveAndDeleteEvents`, `purgeInstance` and the webhook API
  key create/update handlers nested `pop.Connection.Transaction` inside the
  pop middleware transaction, committing the shared outer transaction early
  and leaving the middleware's own commit to fail with a spurious
  `transaction has already been committed or rolled back` ERROR log.
  Fixed via `actions/tx_helper.go` `withTx`: runs the inner function
  directly when the connection is already transactional. Verified: no such
  error in the log after the fix.

## Test approach & validation

- **Unit/handler tests** (`actions/events_delete_test.go`, SQLite):
  - `TestEventsDeleteProcessedScopeOnlyTouchesProcessedEvents` — wrong
    confirmation → 422 + no deletion + no archive; `DELETE PROCESSED` → only
    the 3 processed fixture events deleted, the unprocessed one survives,
    archive row has `scope=processed`, 3 valid JSONL lines all with
    `processed_at` set.
  - `TestEventsDeleteProcessedScopeWithNoProcessedEventsWritesNoArchive` —
    empty match → redirect, no archive row.
  - All pre-existing delete/archive tests still pass.
  - `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...` green.
- **Code quality** (each run separately): `go vet` clean, `staticcheck`
  clean, `gocognit -over 15 .` 6 warnings (same set as pre-change, the
  touched function's score *decreased*), `gocyclo -over 12 .` only the
  pre-existing `archiveAndDeleteEvents` (now 14, was 16).
- **E2E (agent-browser, http://127.0.0.1:3001)**: logged in as admin;
  `/events/delete` shows "Processed events only" / "Uniquement les événements
  traités" (EN + FR checked) with the `DELETE PROCESSED` hint; wrong
  confirmation → 422 page, 0 rows touched; `DELETE PROCESSED` on 10123
  dev rows (10122 processed) → flash "Deleted 10122 event(s); archive …
  stored in the database", only the unprocessed row survived, archive listed
  on `/events/archives` and downloaded via
  `/events/archives/<id>/download` (10122 lines, all valid JSON, all
  `processed_at` set). Dev database restored from a pre-test mysqldump
  afterwards; test archive rows removed.

## Commit

`cb9892a` feat(events): add processed-events cleanup scope to admin event
deletion
