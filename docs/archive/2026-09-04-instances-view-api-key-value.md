# Instances admin view shows each API key value (archive)

Status: implemented (2026-09-04).

## Problem

The admin instances view (`/instances/{id}`) listed each restricted API key by
name plus a masked `creaves_<prefix>…` fragment only. The raw key value was
reachable just from the API Keys pages, so an administrator inspecting an
instance could not see which secret that instance actually uses.

## Plan

- Reuse the retained `webhook_api_keys.key_value` column (added by the
  api-key-visibility fix) instead of new storage — the value is already
  persisted and excluded from JSON serialization.
- In all four instances show templates (en/fr/de/nl), render the full raw
  value when `key.KeyValue` is valid; otherwise keep the prefix fallback with
  a localized "value unavailable" note, matching the API Keys pages.
- No action/controller change needed: `loadInstanceAdminView` already loads
  the full `WebhookAPIKeys` rows, `KeyValue` included.
- Tests: extend the locale-template guard to require `key.KeyValue` in every
  instances show template; add an HTTP test proving a modern key renders its
  full value while a legacy key falls back to prefix + note.

## Changes

- `templates/instances/show.plush{,.fr,.de,.nl}.html` — full value when
  stored, otherwise `creaves_<prefix>…` + localized unavailable note.
- `actions/instances_test.go` — locale template guard extended
  (`key.KeyValue`); new `TestInstanceShowRendersAPIKeyValues` covering both
  the retained-value and legacy-no-value paths; test app now mounts
  `GET /instances/{instance_id}`.
- `bugs.md` — "instances view should show API key" TODO removed.

## Validation

- `CGO_ENABLED=1 go test -tags sqlite -count=1 ./...` — pass (actions,
  models).
- `go vet ./...` — pass. `staticcheck ./...` — pass.
- Focused run `-run 'TestInstance'` confirms the new value/fallback
  assertions before the full suite.
