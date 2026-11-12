# Creaves Console

Creaves Console is the central, read-only consolidation application for multiple Creaves wildlife-care installations. It receives animal lifecycle events over authenticated webhooks, maintains one consolidated record per `(instance_id, animal_id)`, and provides cross-center dashboards, reports, drill-down history, and export views.

Console is a separate Go module with its own database and users. It shares neither code nor a database with Creaves. The legacy direct-database pull model has been removed; **authenticated webhook push is the only ingestion path**.

## Current features

- **Multi-center consolidation**: instance registry, automatic registration, globally scoped or per-instance views, and duplicate-safe event processing.
- **Animal overview**: current lifecycle state, taxonomy, discovery/intake/outtake details, localized reference values, event counts, and source timestamps.
- **Reporting**: dashboard statistics, annual/location/type/species reports, drill-down event history, and 22 configured export queries with CSV output.
- **Sync administration**: instance-scoped API keys, event inspection, bounded event deletion with JSONL archive, per-instance cleanup, delivery/checksum status, and resync monitoring.
- **Recovery**: idempotent redelivery, state-hash acknowledgements, poison-event skip-and-continue processing, delete tombstones, and cleanup-to-full-resync rebuilding.
- **Access control and localization**: independent session users, admin-only maintenance routes, CSRF protection, and English/French/German/Dutch UI variants.

Console stores the current synchronization contract, not a complete copy of each center's care database. Full care plans, treatment administration, attachments, and veterinary workflows remain in Creaves.

## Architecture

```text
Creaves center A ─┐
                  ├─ POST /webhook/events ─▶ event_streams ─▶ EventProcessor
Creaves center B ─┘        Bearer API key                         │
                                                                   ▼
                                                   consolidated_animals / creaves_instances
                                                                   │
                                                    dashboard, reports, exports, drill-down
```

| Area | Responsibility |
| --- | --- |
| `actions/` | HTTP handlers, webhook authentication, event processing, consolidation, reports, exports, and sync administration |
| `models/` | Pop models and event-to-consolidated-state mapping |
| `migrations/` | Console schema and migration history |
| `grifts/` | Admin seed, processing/statistics tasks, fixtures, and guarded cleanup |
| `cmd/app/` | Buffalo web/server binary |
| `cmd/cli/` | Operational CLI for serving, processing, rebuilding, statistics, and run history |
| `templates/`, `locales/` | Embedded Plush UI and four locale catalogs |
| `docs/specs/` | Canonical cross-project webhook contract |
| `docs/archive/` | Historical research, pull-model material, plans, and closed fix rounds |

Webhook processing is synchronous within the request transaction. Each event UUID is unique. Valid duplicate deliveries are acknowledged without applying duplicate side effects; an existing unprocessed event can be reprocessed. Partial responses identify exactly which events Console accepted so Creaves leaves all other events retryable.

## Requirements

- Go **1.26** (see `go.mod`)
- Buffalo CLI: `go install github.com/gobuffalo/cli/cmd/buffalo@latest`
- MySQL 8 or MariaDB for development and production
- CGo and a C compiler for the SQLite test harness
- Docker with Buildx for optional container builds

No Node.js asset pipeline is required; templates and public assets are embedded with `go:embed`.

## Local setup

> Use an isolated database for development and tests. Console cleanup, rebuild, deletion, and fixture commands are not safe to run against an unreviewed production database.

1. Install Go dependencies and create a local database matching `database.yml`:

   ```bash
   go mod download
   ```

   ```sql
   CREATE DATABASE consolidation CHARACTER SET utf8mb4;
   CREATE USER 'creaves'@'localhost' IDENTIFIED BY 'creaves';
   GRANT ALL ON consolidation.* TO 'creaves'@'localhost';
   ```

   Adjust the user and credentials for the local installation; do not reuse development defaults in production.

2. Migrate, create the initial administrator, and start the server:

   ```bash
   buffalo pop migrate up
   buffalo task db:seed
   buffalo dev
   ```

3. Open **http://127.0.0.1:3001**. The initial login is `admin` / `admin123`; change it before any shared or production deployment.

## Configuration

| Setting | Purpose |
| --- | --- |
| `GO_ENV` | `development`, `test`, or `production`; defaults to `development` |
| `DATABASE_URL` | Production MySQL/MariaDB URL; include `parseTime=true` and `multiStatements=true` |
| `POP_PATH` | Directory containing an alternate `database.yml` for isolated runs |
| `ADDR` | Listen address; development defaults to `127.0.0.1`, containers use `0.0.0.0` |
| `PORT` | HTTP port; default `3001` |
| `DEFAULT_TZ` | Container timezone; default `Europe/Brussels` |
| `CONFIRM=cleanup` | Required only by the destructive `db:cleanup` task |

The provided `docker-compose.yml` uses the `consolidation` database/user and publishes MySQL on host port `3307`; the application itself uses port `3001`.

## Connecting a Creaves center

1. Sign in as an administrator and open **Admin → Webhook API Keys → New**.
2. Enter a descriptive name and the **exact Creaves instance ID**. One key belongs to one center; do not share it between centers.
3. Generate the key and copy the raw `creaves_<uuid>` value from the confirmation page.
4. In that Creaves installation, open **Administration → Synchronization**, verify its instance ID, and add an enabled target with:
   - webhook URL `https://<console-host>/webhook/events`;
   - the generated Bearer API key;
   - reviewed batch-size and rate-limit settings.
5. Save the target and create or update an animal in Creaves.
6. Verify delivery in Creaves and confirm the instance, animal, and event history in Console.

API keys are stored as bcrypt hashes; the raw value is also retained for the existing administrative reveal workflow. Protect admin sessions and do not commit keys or URLs containing credentials.

## Webhook contract

`POST /webhook/events` accepts `Authorization: Bearer creaves_<uuid>` and a JSON envelope. Contract v2 adds `contract_version`, an `instance` block, optional `sync` metadata, optional resync IDs, deterministic `animal_state` events, localized reference values, and state hashes. V1-shaped events remain supported.

| Event | Consolidation behavior |
| --- | --- |
| `animal_discovered` | Merge discovery, intake, taxonomy, and current-state fields |
| `animal_status_changed` | Merge the status transition and supplied state |
| `animal_released` | Merge outtake data and set `current_status=released` |
| `animal_died` | Merge outtake data and set `current_status=died` |
| `animal_state` | Replace contract-managed fields with a full current-state snapshot |
| `animal_deleted` | Delete the consolidated row and its source event history; retain a tombstone to reject stale older events |

A successful or partially successful response contains `processed`, `total`, and `processed_ids`. State events can also return `confirmed` IDs with the stored state hash. Events with invalid instance/key relationships or payloads are reported in `errors` and are not acknowledged.

See [Sync Specification v2](docs/specs/SYNC_SPEC.md) for envelope fields, ordering, checksums, status rules, and backward compatibility. Any contract change must be made and tested in both repositories.

## Operational routes

| Route | Access | Purpose |
| --- | --- | --- |
| `/`, `/dashboard` | Session | Global/per-instance status and counts |
| `/consolidated_animals` | Session | Consolidated animal search and filtering |
| `/consolidated_animals/:id/drill_down` | Session | Source event history and sync details |
| `/reports`, `/export/reports` | Session | Scoped reports and exports |
| `/instances` | Session | Registered centers and last-seen state |
| `/sync_management` | Admin | Checksums, expected totals, and destructive cleanup controls |
| `/events` | Admin | Received event inspection and deletion workflows |
| `/webhook_resync` | Admin | Resync status and monitoring |
| `/webhook_api_keys` | Admin | Instance-scoped key management |
| `/users` | Admin | Console user administration |

## Tasks and CLI

Normal webhook processing is synchronous; these commands are for maintenance and recovery.

| Command | Purpose |
| --- | --- |
| `buffalo task consolidation:process` | Process unprocessed events, continuing after poison records |
| `buffalo task consolidation:rebuild` | Clear the consolidated view and replay retained events |
| `buffalo task consolidation:stats` | Print animal and unprocessed-event statistics |
| `buffalo task db:seed` | Create the default admin if absent |
| `CONFIRM=cleanup buffalo task db:cleanup` | Delete Console application data while preserving migration history |

The standalone CLI wraps the same operations:

```bash
go build -o bin/consolidation-cli ./cmd/cli
./bin/consolidation-cli process
./bin/consolidation-cli rebuild
./bin/consolidation-cli stats
./bin/consolidation-cli history
./bin/consolidation-cli serve
```

`import` is a legacy command name for a recorded processing run over already-received events; it does not pull source databases. `import -dry-run` reports the pending count without writing an import run or changing events. Rebuild and cleanup are destructive maintenance operations: back up first and use the recovery walkthrough.

## Testing

Console tests use **SQLite** and require CGo plus the `sqlite` build tag:

```bash
CGO_ENABLED=1 go test -tags sqlite -count=1 ./...
CGO_ENABLED=1 go test -tags sqlite -count=1 ./actions -run TestName -v
go vet ./...
```

Coverage includes webhook authentication, idempotency, partial responses, event ordering, deletion tombstones, multi-instance processing, report scoping, export SQL, and push/receive integration. For schema, route, template, or model changes, also run authenticated browser checks in every affected locale.

The cross-application setup is in [Creaves Setup & Testing](../creaves/docs/SETUP_AND_TESTING.md). The cleanup/resync procedure is in [the recovery walkthrough](docs/e2e-cleanup-resync-walkthrough.md).

## Build and deployment

```bash
buffalo build --environment production -o bin/creaves-console
export GO_ENV=production
export DATABASE_URL='mysql://user:password@(host:3306)/consolidation?parseTime=true&multiStatements=true'
./bin/creaves-console migrate
./bin/creaves-console task db:seed
./bin/creaves-console
```

Back up the database before upgrades and before rebuild/cleanup operations. Apply migrations on a clone first, keep the previous binary available, then verify webhook authentication and processing before reopening delivery.

### Docker

```bash
docker compose up -d --build
# Console: http://127.0.0.1:3001
```

The container entrypoint runs migrations and `db:seed`, then starts the server. The compose file contains development credentials; replace them and review exposed ports before production use.

Operational helpers:

```bash
./docker-run.sh process
./docker-run.sh rebuild
./docker-run.sh stats
./docker-run.sh serve
```

## Troubleshooting

- **401**: wrong or inactive key.
- **403**: the key's instance ID does not match the event/envelope instance ID.
- **Events received but no animal appears**: inspect event errors and run `consolidation:process`; check logs before replaying everything.
- **Center state diverges**: compare announced totals/checksums in Sync Management, then perform a reviewed cleanup/full-resync recovery.
- **Stale out-of-order update**: confirm event timestamps and tombstone handling; do not delete retained history as a first response.
- **SQLite tests fail to compile**: use `CGO_ENABLED=1` and `-tags sqlite`.

## Documentation and contributing

Start with [the documentation index](docs/README.md). `AGENTS.md` contains concise engineering and safety rules. Current contracts, operating guides, and design notes live outside `docs/archive/`; historical research, plans, pull-model documentation, and fix reports are archived and are not current instructions.

Preserve idempotency, per-instance isolation, backward-compatible payloads, all four UI locales, and explicit confirmation for destructive operations. Add receiver and producer tests for every contract change.

No repository-level license file is currently provided; contact the project owner before redistribution.
