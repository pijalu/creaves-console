# API key visibility fix (archive)

Status: implemented.

## Problem

Administrators could only see the raw webhook API key immediately after creation. The key was not retrievable from its API key page.

## Fix

- Added nullable `key_value` column and model field.
- Persist generated raw key for administrator retrieval.
- Display key on API key show page while keeping it excluded from JSON serialization.
- Added migration, schema snapshot, and regression assertions.

## Validation

- `CGO_ENABLED=1 go test -tags sqlite ./actions/... ./models/...` — pass.
