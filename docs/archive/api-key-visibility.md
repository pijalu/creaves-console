# API key visibility fix (archive)

Status: implemented (extended after review: key must be *fully* visible, never masked).

## Problem

Administrators could only see the raw webhook API key immediately after creation. The key
was not retrievable from its API key page, and the first implementation still showed a
truncated `creaves_<prefix>…` form on the index and show pages.

## Fix

- Added nullable `key_value` column and model field.
- Persist generated raw key for administrator retrieval.
- Index page (`/webhook_api_keys`) shows the **full raw key value** in each row
  (legacy keys without a stored value fall back to the prefix with a
  "value unavailable" note).
- Show page shows the full raw key value; the redundant masked "Key Prefix" row was
  removed (legacy keys show an unavailable note including their prefix).
- Created page no longer claims the key is one-time-only; it now states the key stays
  retrievable from the API Keys pages.
- Key value remains excluded from JSON serialization (`json:"-"`).
- Lint cleanups in the same pass: lowercased `fmt.Errorf` strings (ST1005) in
  `actions/instances.go`, `actions/users.go`, `actions/webhook_api_keys.go`;
  removed unused `models.nullableString` (U1000), unused `scopedWhere` and duplicate
  `log` import.

## Validation

- `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...` — pass
  (actions 57.0%, models 62.5% coverage).
- `go vet ./...` — pass. `staticcheck ./...` — pass.
- `gocognit -over 15 .` — only pre-existing warnings in untouched code
  (WebhookEventsHandler, UpdateFromPayload, installSafePopTxLogger, DashboardIndex).
- `gocyclo -over 12 .` — only pre-existing warnings in untouched code.
- agent-browser validation (dev server on :3001, admin session):
  - Created key "BrowserCheck" via the UI → created page renders the full key
    `creaves_793429d9-d36f-42eb-ad5f-51e57c6ffdb2` in the copy input (verified via DOM),
    Copy button handler copies `input.value` in full (headless clipboard *read* is
    permission-blocked, so the handler + input value were inspected instead).
  - Index page row shows the full value, byte-identical to `webhook_api_keys.key_value`
    in MySQL.
  - Show page shows the full value once; computed style check:
    `overflow: visible`, `scrollWidth == clientWidth` (no CSS truncation/clipping).
  - Legacy key ("La Grange Sauvage", `key_value` NULL) renders the unavailable note
    with its prefix hint on both index and show.
