# Fix plan — Storage: all storage in the DB (no files outside the DB)

Date: 2026-09-05
Source: workspace `bugs.md` → "creaves-console: storage"

## Bug report

Make sure all storage occurs in the DB — there should not be any files outside
the DB (no outside jsonl for events or similar).

## Root cause

`actions/events_delete.go` (`EventsDeleteCreate` → `archiveAndDeleteEvents` →
`writeEventArchive`) serializes events matched by the admin deletion scope to a
JSONL file under `archives/event-deletions/` (overridable via
`EVENT_ARCHIVE_DIR`) before deleting the rows. This is the only place the
console persists application data to the filesystem (verified by repo-wide
search for `os.Create`, `os.OpenFile`, `os.WriteFile`, `os.MkdirAll`,
`MultipartForm`, `FormFile`).

## Change

Replace the file archive with a DB table that stores the same JSONL payload
as a row — archive semantics (write-before-delete, failure blocks deletion)
are preserved, but storage is 100% in the DB.

1. **Migration** (fizz, runs from scratch; console has no production so no
   backfill of old files needed — existing `archives/` files are orphaned
   operator data, removed from the repo tree if present):
   `event_stream_archives`:
   - `id` uuid PK
   - `scope` varchar(16) — `all` | `instance`
   - `instance_id` varchar(255) nullable — set for instance scope
   - `event_count` int
   - `content` longtext — JSONL, one full event JSON per line (same bytes the
     file used to contain)
   - `created_at`, `updated_at` datetime
2. **Model** `models/event_stream_archive.go`: `EventStreamArchive` struct +
   validation.
3. **`actions/events_delete.go`**:
   - `writeEventArchive` → `storeEventArchive(tx, events, scope, instanceID)`
     building the identical JSONL in a buffer and inserting a row inside the
     same DB transaction as the DELETE (single `tx.Transaction` covering both
     archive insert + delete → atomic: archive failure rolls back the delete
     and vice versa; strictly safer than the old two-step file flow).
   - `archiveAndDeleteEvents` returns the archive record ID instead of a file
     path; flash message references the archive record (e.g. "archive #<id>"),
     not a path.
   - Remove `archiveRootDir`, `sanitizeArchiveToken`, `EVENT_ARCHIVE_DIR`
     handling, and the `os`, `path/filepath`, `regexp` imports.
4. **Templates** `templates/events/delete.plush.{html,de,fr,nl}.html` and
   `templates/events/index.plush.{html,nl}.html` (+de/fr if they mention
   JSONL): update copy from "archived to JSONL files under
   archives/event-deletions/" to "archived in the database
   (event_stream_archives), downloadable below".
5. **Download route**: `GET /events/archives` (list, admin) and
   `GET /events/archives/:id/download` returning the JSONL content as
   `application/x-ndjson` attachment — keeps the operator escape hatch of
   getting the archive out, now sourced from the DB.
6. **Tests** `actions/events_delete_test.go`: rewrite archive assertions from
   file-system checks to DB checks:
   - successful deletion inserts one archive row whose content parses as
     JSONL with the deleted events;
   - failed confirmation writes no archive row;
   - archive row + deletion are atomic (empty match → no archive row);
   - download endpoint returns the stored JSONL.
   Test schema setup (`TestMain` in `event_processor_test.go` creates tables
   manually for SQLite) must create `event_stream_archives` too.

## Test approach

- Unit: rewritten `events_delete_test.go` + full suite
  `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...`.
- Migration from scratch on the dev MySQL DB: `buffalo pop migrate reset`
  (console dev DB may be wiped) + `buffalo task db:seed`.
- Browser (agent-browser, admin/admin123 @ http://localhost:3001):
  inject a test event via the webhook endpoint with an API key, run the admin
  delete flow for that instance, confirm:
  - flash message mentions DB archive (no path);
  - no `archives/` directory is created (cwd of the dev process);
  - `/events/archives` lists the archive and the download returns JSONL.

## Validation steps

1. Migration runs from scratch; server boots without template errors.
2. Browser flow above passes; filesystem check shows no new files.
3. Quality gates (each separately): `go vet ./...`, `staticcheck ./...`,
   `gocognit -over 15 .`, `gocyclo -over 12 .`,
   `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...`.
4. Commit; move bug + plan to `docs/archive/`; remove section from `bugs.md`.

## Issues found during testing (fixed)

- Plush templates cannot parse a method chain with two calls
  (`a.CreatedAt.UTC().Format(...)` → "no prefix parse function for DOT
  found"). Fixed by using `a.CreatedAt.Format(...)` (single call) with a
  literal "UTC" suffix in `templates/events/archives.plush.*.html`.
- Test app `newEventsTestApp` mounted only the pre-existing events routes;
  the new archives routes had to be registered before
  `/events/{event_id}` (Buffalo would otherwise route `/events/archives` to
  `EventShow` → 404).
- `pop.Connect("sqlite3://:memory:")` does not work in tests ("could not
  find connection named …"); scratch rollback test uses
  `pop.NewConnection(&pop.ConnectionDetails{Dialect: "sqlite", Database: ":memory:"})`
  instead.

## Validation results (2026-09-05)

- Unit tests: rewritten `actions/events_delete_test.go` (12 archive-related
  tests) — archive row written with parseable JSONL containing exactly the
  deleted events; failed confirmation / empty match write no archive row;
  forced archive failure (missing table in scratch DB) leaves events
  untouched; index + download endpoints return stored JSONL with
  `Content-Disposition: attachment`. Full suite:
  `ok creaves-console/actions 59.0%`, `ok creaves-console/models 58.4%`.
- Migration applied to dev MySQL: `> create_event_stream_archives` —
  `Successfully applied 1 migrations`.
- Browser (agent-browser, admin/admin123 @ http://127.0.0.1:3001):
  - `/events` danger-zone copy now reads "archived in the database first".
  - `/events/delete` copy mentions DB archive and links to
    `/events/archives`.
  - Delete flow (scope=instance, instance_id=browser-check, 2 events):
    redirect to `/events`, flash
    `Deleted 2 event(s); archive cbf4e96f-dde3-4717-b9dc-e94bef0e5e64 stored in the database`.
  - `/events/archives` lists the archive (scope=instance,
    instance=browser-check, events=2).
  - Download endpoint returns the 2 events as JSONL (verified content).
  - `/events` shows 0 events remaining; MySQL confirms
    `event_stream_archives` row (instance, browser-check, 2) and 0
    `event_streams` rows.
  - No `archives/` directory exists in the repo/process tree after the run.
- Quality gates (each run separately):
  - `go vet ./...` — clean.
  - `staticcheck ./...` — clean.
  - `gocognit -over 15 .` — only pre-existing warnings (UpdateFromPayload 38,
    installSafePopTxLogger 25, TestOutcomeHelpersAcceptValueAndPointer 23,
    DashboardIndex 17, SyncManagementIndex 16); unchanged baseline.
  - `gocyclo -over 12 .` — same pre-existing baseline (UpdateFromPayload 37,
    LocalizedField 21, DashboardIndex 17, SyncManagementIndex 14,
    applyConsolidatedAnimalFilters 14, installSafePopTxLogger 13,
    UpsertByInstanceID 13, EventsDeleteCreate 13). EventsDeleteCreate was on
    the pre-existing list and was simplified by this change (still 13).
  - `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...` — pass.
