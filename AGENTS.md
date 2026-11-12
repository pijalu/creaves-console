# Creaves Console — Agent Guide

## Purpose and boundaries

Creaves Console is the central, read-only consolidation application. It receives authenticated webhook events from independent Creaves installations, maintains one consolidated row per `(instance_id, animal_id)`, and serves dashboards, reports, exports, and source-event drill-down.

Console is the **receive side**. It has no shared code or database with Creaves and no direct source-database ingestion. Use only `POST /webhook/events`; keep the producer and receiver contract changes synchronized in both repositories.

## Read first

| Document | Use |
| --- | --- |
| [README.md](README.md) | Current features, setup, operations, CLI, deployment, and troubleshooting |
| [docs/specs/SYNC_SPEC.md](docs/specs/SYNC_SPEC.md) | Canonical webhook v2 contract and compatibility rules |
| [docs/e2e-cleanup-resync-walkthrough.md](docs/e2e-cleanup-resync-walkthrough.md) | Current cleanup/full-resync recovery procedure |
| [../creaves/docs/SETUP_AND_TESTING.md](../creaves/docs/SETUP_AND_TESTING.md) | Combined local setup and cross-application validation |
| [docs/README.md](docs/README.md) | Curated documentation index |
| `docs/archive/` | Historical pull-model material, research, plans, and closed fixes; not current instructions |

Keep this guide short. Do not duplicate the complete payload schema, route inventory, database catalog, or historical bug narratives.

## Stack and commands

- Go **1.26**, Buffalo 1.1, Pop v6, Plush, MySQL/MariaDB.
- Development port: **3001**.
- Tests use SQLite with CGo and the `sqlite` build tag; development and production use MySQL/MariaDB.
- Install Buffalo with `go install github.com/gobuffalo/cli/cmd/buffalo@latest`.

```bash
go mod download
buffalo pop migrate up
buffalo task db:seed
buffalo dev
```

Development login: `admin` / `admin123`; change it outside a disposable environment. `db:seed` creates only the initial admin if absent.

Use `POP_PATH=/absolute/config-directory` to select an alternate `database.yml`. Production normally uses `DATABASE_URL`. Verify the selected database before migration, cleanup, rebuild, deletion, or fixture operations.

## Testing requirements

```bash
CGO_ENABLED=1 go test -tags sqlite -count=1 ./...
CGO_ENABLED=1 go test -tags sqlite -count=1 ./actions -run TestName -v
go vet ./...
```

Test authentication, instance scoping, idempotency, partial responses, ordering, deletion tombstones, poison-event recovery, and export/report behavior when changing those paths. Template, action, model, route, and schema changes also require authenticated browser checks. Exercise every affected locale (`en-US`, `fr`, `de`, `nl`) and both success and error paths.

Never weaken or skip tests to obtain a green result. Historical E2E records in `docs/archive/` do not replace current validation.

## UI and localization

Every user-facing UI change must update all four localized template variants and translation catalogs. Canonical values stored in event payloads remain French/base-locale keys; optional `translations` maps supply display text without changing report grouping or identity.

## Processing and contract invariants

- API keys are instance-scoped. A key/instance mismatch is rejected; do not derive trust from the envelope instance block.
- Event UUIDs are unique. Duplicate processed events are successful no-ops; duplicate unprocessed events may be reprocessed.
- Processing is synchronous. Responses identify exactly which event IDs were accepted; state events can also return stored state hashes for acknowledgement.
- Partial failures must not acknowledge failed events. Poison records must not block newer processable events during replay.
- Preserve event ordering protections. Older out-of-order updates must not clobber newer consolidated state.
- `animal_deleted` removes the source animal and retained event history and leaves tombstone protection against stale older events.
- `animal_state` replaces contract-managed fields; lifecycle events merge their respective changes. Canonical status values are `in_care`, `released`, and `died`.
- Instance registration and reporting are global or per-instance. Never leak rows across an instance-scoped report or cleanup.
- Expected totals/checksums come from the producer announcement, not from a count derived from the receiver's current rows.

Use the canonical specification for exact wire shapes and response examples.

## Destructive operations

`consolidation:rebuild`, instance cleanup, event deletion, and `db:cleanup` are maintenance operations, not normal ingestion. Require explicit scope, a backup, and a recovery plan. `db:cleanup` additionally requires `CONFIRM=cleanup` and preserves migration history.

Prefer the documented single-instance cleanup/full-resync path over ad-hoc SQL. Event deletion writes a JSONL archive before removal. Never run fixture or cleanup commands against an unreviewed production database.

## Operational commands

```bash
buffalo task consolidation:process
buffalo task consolidation:rebuild
buffalo task consolidation:stats
go build -o bin/consolidation-cli ./cmd/cli
./bin/consolidation-cli history
```

The CLI `import` name is retained for a recorded processing run over received events; it does not restore the removed pull model. Its `-dry-run` option is read-only. Docker helpers in `docker-run.sh` wrap the common tasks.

## Deployment and security

The production container runs migrations and `db:seed`, then starts the server. Replace compose development credentials, change the default admin password, protect raw API-key reveal pages with admin access, and keep secrets out of source control. Back up the database before upgrades and validate webhook processing before reopening delivery.

## Change checklist

1. Determine whether the change is receiver-only or alters the shared contract.
2. Add regression tests for idempotency, scoping, ordering, and error paths as applicable.
3. Preserve all four UI locales and canonical payload values.
4. Validate destructive maintenance on an isolated database with a rollback path.
5. Run the SQLite suite and authenticated browser checks as applicable.
6. Update only current guides; archive research, implementation plans, and dated results.
