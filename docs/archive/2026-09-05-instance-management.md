# Fix plan — Instance management: API keys must belong to an instance

Date: 2026-09-05
Source: workspace `bugs.md` → "creaves-console: instance management does not work"

## Bug report

After removing all instances, creating an API key did not create a new
instance: `http://localhost:3001/instances/LaGrange` returns 404
("mysql select one: sql: no rows in result set").

The API key should be part of an instance record (multiple API keys per
instance). An API key must always be associated with an instance — it must
not be possible to create an API key without an instance.

## Root cause

1. `webhook_api_keys.instance_id` is nullable
   (`DEFAULT NULL`, see `migrations/schema.sql`) and the model validation
   (`models/webhook_api_key.go: Validate`) does not require it — keys can
   exist with no instance ("Any" scope).
2. `WebhookAPIKeysResource.Create` (`actions/webhook_api_keys.go:86`)
   inserts only the key row; it never creates a `creaves_instances` record.
   Instances appear only lazily when webhook events arrive
   (`models.UpsertByInstanceID` from `actions/webhook.go`).
3. Admin cleanup (`purgeInstance` in `actions/instances.go`) deletes the
   instance **and** its keys. So after cleanup + key re-creation, no
   `creaves_instances` row exists and `/instances/:instance_id` 404s.

## Change

Make the key→instance association mandatory and create the instance row
atomically with the key (console has no production — schema redesign OK).

1. **Migration** `20260905130000_instance_keys_require_instance.up.sql`
   (raw SQL, MySQL + SQLite-compatible for tests via manual schema):
   - `ALTER TABLE webhook_api_keys MODIFY instance_id VARCHAR(255) NOT NULL;`
     (SQLite tests create tables manually in TestMain, so no SQLite flavor
     needed there; dev MySQL reset will replay this.)
   - `ALTER TABLE webhook_api_keys ADD CONSTRAINT fk_webhook_api_keys_instance
     FOREIGN KEY (instance_id) REFERENCES creaves_instances(instance_id);`
     — enforced at DB level. Down migration drops the FK and re-allows NULL.
     Note: `creaves_instances.instance_id` has a UNIQUE index, so it is a
     valid FK target on MySQL.
2. **Model** `models/webhook_api_key.go`: add
   `validators.StringIsPresent{Field: w.InstanceID, Name: "InstanceID"}` to
   `Validate`. Add `belongs_to` association comment; keep struct field a
   string (webhook hot path compares `key.InstanceID` per event — keep it).
3. **`WebhookAPIKeysResource.Create`**: after validation, run in one
   `tx.Transaction`:
   - `models.UpsertByInstanceID(tx, key.InstanceID, key.Name, "", now)` —
     creates the instance row if missing (this fixes the 404: the instance
     exists immediately after key creation, before any event arrives);
   - create the key.
   If the key fails validation (empty instance_id), render the form again
   with errors (no instance row created — upsert happens only on success).
4. **Templates** `webhook_api_keys/new.plush.{html,de,fr,nl}.html`: label
   "Instance ID (required)" — creating the key creates/attaches the
   instance. `edit.plush.*`: keep field (reassigning a key to another
   instance must also upsert — handled in Update, same transaction pattern).
   `index.plush.*`: instance link now always present (drop the "Any" branch).
5. **`WebhookAPIKeysResource.Update`**: same upsert when `InstanceID`
   changes (single transaction: upsert new instance + update key). Purge
   (`purgeInstance`) still deletes instance + its keys together — that
   behavior is intended ("After removing all instances" the admin re-creates
   the key, which now re-creates the instance row).
6. **Tests** `actions/webhook_api_keys_test.go`:
   - Create without instance_id → 422, no key row, no instance row.
   - Create with instance_id → key + `creaves_instances` row exist
     (`/instances/<id>` data loadable via `loadInstanceAdminView`); a second
     key for the same instance succeeds (multiple keys per instance).
   - Update moving a key to a new instance upserts the new instance.
   - Test schema (TestMain in `event_processor_test.go`): add the FK to the
     manually created `webhook_api_keys` table (SQLite accepts FK syntax;
     `PRAGMA foreign_keys` stays off in tests unless enabled — model-level
     validation is what tests primarily exercise).

## Test approach

- Unit: `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...`
  (updated webhook_api_keys tests + full suite regression).
- Migration from scratch on dev MySQL: `buffalo pop migrate reset` +
  `buffalo task db:seed` (console dev DB may be wiped).
- Browser (agent-browser, admin/admin123 @ http://localhost:3001):
  1. Clean state: purge/delete instance data (or use a fresh dev DB).
  2. Admin → Webhook API Keys → New: leave instance empty → form re-renders
     with validation error, no key created.
  3. Create key with instance "LaGrange" → key created, raw key shown once.
  4. Open `/instances` → LaGrange listed; open `/instances/LaGrange` →
     200 with instance detail (no 404).
  5. Create a second key for LaGrange → both keys listed on the instance
     page (KeyCount 2).
  6. Instances index link from `/webhook_api_keys` row works.

## Validation steps

1. Migration runs from scratch; server boots without template errors.
2. Browser flow above passes (screenshots/text captured via agent-browser).
3. Quality gates (each separately): `go vet ./...`, `staticcheck ./...`,
   `gocognit -over 15 .`, `gocyclo -over 12 .`,
   `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...`.
4. Commit; move bug + plan to `docs/archive/`; remove section from
   `bugs.md`.

## Issues found during testing (fixed)

- `Update` bound the form over the loaded key: when `InstanceID` is omitted
  (JSON API clients), `c.Bind` zeroed it and the new validation rejected the
  update with 422. Fixed by preserving the stored `InstanceID` when the form
  omits the field (`actions/webhook_api_keys.go` Update).
- The migration up-file additionally backfills `creaves_instances` rows for
  keys whose instance was purged (so the FK can be added on non-empty dev
  DBs) and deletes legacy keys with empty `instance_id` (they violate the
  new rule; console has no production data).
- Existing tests seeding keys with an empty instance through
  `seedAPIKey(t, tx, "")` were updated to `"inst-seed"` in
  `webhook_api_keys_test.go` (resource-level tests). Webhook handler tests
  keep `""` seeds on purpose: the receiver still accepts unscoped legacy
  keys for backward compatibility (test schema has no FK enforcement).

## Validation results (2026-09-05)

- Unit: 4 new tests — create without instance → 422 (no key, no instance);
  create with instance → instance row exists + `loadInstanceAdminView`
  loads (the 404 path); second key for the same instance succeeds
  (KeyCount 2, instance not duplicated); update to a new instance registers
  it. Full suite green: actions 59.0%, models 58.4%.
- Migration from scratch on dev MySQL (`buffalo pop reset` + `db:seed`):
  `instance_id` is `varchar(255) NOT NULL` with FK
  `fk_webhook_api_keys_instance → creaves_instances(instance_id)`
  (verified in information_schema).
- Browser (agent-browser, admin/admin123 @ http://127.0.0.1:3001):
  - `/webhook_api_keys/new`: Instance ID labelled "(required)"; submitting
    with empty instance re-renders the form (server-side 422, verified via
    DB counts — no key, no instance created).
  - Create "LaGrange key" / instance "LaGrange" → redirect to one-time
    created page (raw key shown once).
  - `/instances` lists LaGrange; `/instances/LaGrange` returns **200** with
    instance detail and "Restricted API keys: 1" (previously 404).
  - Second key "LaGrange key 2" → `/instances/LaGrange` shows 2 keys.
  - `/webhook_api_keys` rows link to `/instances/LaGrange`; no "Any"
    entries.
- Quality gates (each run separately):
  - `go vet ./...` — clean.
  - `staticcheck ./...` — clean.
  - `gocognit -over 15 .` — unchanged pre-existing baseline
    (UpdateFromPayload 38, installSafePopTxLogger 25,
    TestOutcomeHelpersAcceptValueAndPointer 23, DashboardIndex 17,
    SyncManagementIndex 16).
  - `gocyclo -over 12 .` — unchanged pre-existing baseline (incl.
    UpsertByInstanceID 13, EventsDeleteCreate 13 — both untouched in
    complexity by this change).
  - `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...` — pass.
