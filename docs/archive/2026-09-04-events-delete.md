# Plan: Admin deletion of received webhook events

**Date:** 2026-09-04
**Status:** Implemented

## Goal

Admins can permanently delete received webhook events — all of them, or only
the events of one source instance — from the admin UI. Every deletion is
archived to a JSONL file first, so raw event payloads survive even after the
rows are gone.

## Design

### Scope and confirmation (mirrors `InstanceCleanup`)

| Scope | Meaning | Typed confirmation |
|-------|---------|--------------------|
| `all` | every row in `event_streams` | exact string `DELETE ALL` |
| `instance` | rows with the given `instance_id` | exact `instance_id` |

Anything else → `422`, nothing deleted, no archive written.

### Archive before delete

`archiveAndDeleteEvents` selects the full rows matched by the scope,
serializes them one-JSON-per-line to
`<EVENT_ARCHIVE_DIR|archives>/event-deletions/events-<utcstamp>-<scope>[-instance-<id>].jsonl`,
and only then deletes **exactly those IDs** inside one transaction. If the
archive cannot be written (e.g. directory not creatable), the database is
left untouched. An empty match writes no file and skips the DELETE.

`EVENT_ARCHIVE_DIR` overrides the archive root (tests, custom deployments).
`archives/` is gitignored. Instance IDs embedded in file names are sanitized
(`[^A-Za-z0-9._-]` → `_`) so they can never traverse out of the archive
directory. Files are created with `O_EXCL`, so a retry can never clobber an
existing archive.

### Routes

- `GET  /events/delete` → `EventsDeleteNew` (confirmation form; lists known
  instance IDs)
- `POST /events/delete` → `EventsDeleteCreate`

Both are admin-only, CSRF-protected, and registered **before**
`GET /events/{event_id}` because the router matches in registration order —
otherwise `{event_id}` would capture the `delete` segment.

### UI

Danger-zone card on `/events` (all four locales) linking to the form;
`events/delete.plush{,.de,.fr,.nl}.html` with scope select, instance input,
confirmation input.

## Tests (`actions/events_delete_test.go`, sqlite tag)

- Non-admin blocked on both form and POST (403), DB and archive untouched.
- Wrong/missing confirmation and bogus scope → 422, no deletion, no archive.
- Scope `all`: 303, all rows gone, one JSONL file with all 4 events, each
  line valid JSON with id/payload/imported_at and correct instance IDs.
- Scope `instance`: only that instance's rows deleted; archive file name and
  content limited to that instance; other instance survives.
- Zero matches: no archive file created.
- Archive write failure aborts the whole operation (no deletion).
- Unit tests for `sanitizeArchiveToken` (no path separators survive).
- Form route wins over `/events/{event_id}` (rendered form, not 404).

## Validation

`CGO_ENABLED=1 go test -tags sqlite -count=1 ./...`, `go vet ./...`,
`staticcheck ./...` — all green (see commit).
