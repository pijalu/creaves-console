# Events admin view

Status: implemented (2026-09-04).

## Problem

The console stores every received webhook event in `event_streams`, but there
is no admin surface to inspect them. Operators can only see aggregate counts
(dashboard, instances view). When sync misbehaves (missing animals, stale
consolidated rows, processing errors) there is no way to see which events were
received, whether they were processed, and what payload arrived.

## Goal

Dedicated admin view listing received webhook events, plus a per-event detail
view with the full payload, reachable from the admin navigation.

## Plan

- `actions/events.go`:
  - `EventsIndex` (admin): paginated list of `event_streams`, newest first
    (`imported_at desc`), filters by `instance_id`, `event_type`, and
    processed state (`all` / `processed` / `pending`). Filter options
    (distinct instance ids) are loaded for the form. Uses the standard
    `tx.PaginateFromParams` + `paginator` helper like the other list views.
  - `EventShow` (admin): single event, all metadata plus the payload rendered
    as pretty-printed JSON, and a link to the consolidated animal when one
    exists for `(instance_id, animal_id)`.
- `actions/app.go`: routes `GET /events` and `GET /events/{event_id}`
  (session + admin enforced in the handlers, like `/instances`).
- Templates `templates/events/{index,show}.plush{,.fr,.de,.nl}.html`:
  self-contained per-locale pages (no partials), matching the instances
  pages style: index with filter form + table (UUID short, instance link,
  animal, type badge, imported, processed state), show with metadata table
  and `<pre>` payload.
- `templates/application.plush{,.fr,.de,.nl}.html`: add an "Events" entry to
  the admin dropdown.
- Tests `actions/events_test.go` (sqlite tag): non-admin forbidden, index
  renders seeded events with filter + processed-state coverage, show renders
  payload JSON and links the consolidated animal, unknown id → 404, and a
  locale-template guard requiring the payload/instance rendering in all four
  locales.
- Validation: full sqlite suite, `go vet`, `staticcheck`.
- Archive this document to `docs/archive/` when implemented.

## Changes

- `actions/events.go` — `EventsIndex` (filters + pagination) and `EventShow`
  (metadata, pretty-printed payload, consolidated-animal link), both admin
  only; `models.EventTypes` added for the type filter dropdown.
- `actions/app.go` — routes `GET /events`, `GET /events/{event_id}`.
- `templates/events/{index,show}.plush{,.fr,.de,.nl}.html` — new pages.
- `templates/application.plush{,.fr,.de,.nl}.html` — "Events" entry in the
  admin dropdown.
- `actions/events_test.go` — admin guard, listing, all three filters,
  payload/consolidated-link rendering, 404, pagination wiring, locale
  template guards.

## Validation

- `CGO_ENABLED=1 go test -tags sqlite -count=1 ./...` — pass (actions,
  models).
- `go vet ./...` — pass. `staticcheck ./...` — pass.

## Notes

- Plush v4 has no `string()` helper: templates call `.String()` on
  `models.EventType` values instead (the old consolidated animals index uses
  `string(y.Year)` and would fail the same way if that branch rendered).
- A zero `uuid.UUID` is always truthy in plush templates, so the
  consolidated-animal link is passed as a prebuilt path string and checked
  with plain string truthiness.

## Out of scope

- Re-processing / replay of events from the UI (only read views here).
- Any webhook contract change.
