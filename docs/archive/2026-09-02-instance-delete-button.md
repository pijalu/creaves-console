# 2026-09-02 — Admin instance full-delete button with confirmation dialog

## Problem / request
The admin instances index had no way to fully remove an instance's data; the
purge endpoint (`POST /instances/{instance_id}/cleanup`, `InstanceCleanup` +
`purgeInstance` in `actions/instances.go`) existed but was only reachable from
the instance detail page. Request: a per-instance Delete button in the admin
instances index that opens a confirmation dialog showing the number of
animals/events/API keys to be deleted and requires typing the exact
instance_id.

## Fix
- `templates/instances/index.{html,de,fr,nl}.plush.html`: per-row danger
  "Delete" button carrying `data-instance-id` / `data-animals` /
  `data-events` / `data-keys`; one shared Bootstrap modal
  (`#delete-instance-modal`) populated on `show.bs.modal` from the trigger
  button's data attributes; the modal form posts to
  `/instances/{id}/cleanup` and requires `instance_id_confirmation`
  (server enforces the exact match, trimming surrounding whitespace).
- `actions/instances_test.go` (new): sqlite-tagged tests for the index render
  (dialog + counts + confirmation input), 403 for non-admins, 422 on
  missing/mismatched confirmation, 303 + full purge scoped to the target
  instance (animals, events, keys, registry row) with the other instance
  untouched, and 404 for unknown instances.

## CSRF regression found and fixed during validation
Live curl validation exposed that **every** form token rendered as an empty
string (login returned 500 "CSRF token not found in request"). Root cause:
mw-csrf stores the masked token as the context key `authenticity_token`, and
buffalo merges the render helper map *over* the context data — a plush helper
registered under the same name shadows the middleware's string with a
function object, and plush writes function values as empty strings.
Fix: register the helper under the non-colliding name `csrf_token`
(`actions/render.go`) and call `<%= csrf_token() %>` in all 13 form
templates (layout ×4 locales, auth/new ×4, instances index ×4, instances
show). Verified live: login now succeeds and all forms carry valid tokens.

## Validation
- `CGO_ENABLED=1 go test -tags sqlite ./actions/... ./models/...` — pass.
- `go test -count=1 -race -cover ./...` — pass.
- `go vet ./...`, `staticcheck ./...`, `gocognit -over 15 .`,
  `gocyclo -over 12 .` — no findings in new/changed code (pre-existing
  warnings in unrelated files unchanged).
- Live curl (dev server, admin session):
  1. `GET /auth/new` → CSRF token rendered; `POST /auth` admin login → 302.
  2. `GET /instances` → 200; delete button shows
     `data-instance-id="cleanup-live-check" data-animals="2" data-events="1"
     data-keys="0"`; shared modal + `instance_id_confirmation` input present.
  3. `POST /instances/cleanup-live-check/cleanup` with
     `instance_id_confirmation=wrong-id` → **422**, data intact.
  4. Same POST with `instance_id_confirmation=cleanup-live-check` → **303**
     redirect to `/instances`; instance no longer listed; MySQL check:
     creaves_instances 0, consolidated_animals 0, event_streams 0 for that
     instance (e2e fixtures for other instances untouched).

## Status
Tested and closed.
