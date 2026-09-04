# Creaves Console — Agent Guide

## What This Is

Creaves Console is a **standalone consolidation application** that provides a unified,
separated view of animal care data across **multiple geographically-distributed Creaves
instances**.

Each Creaves instance (the main tool at `/creaves`) runs independently with its own
database. They cannot share a database because the centers are physically spread out.
Creaves Console solves this by **receiving events via webhooks** from each instance and
building a single consolidated read-only view for authority oversight and cross-center
reporting.

### Relationship to Creaves (main tool)

```
 ┌──────────────┐   webhook (HTTP POST)   ┌──────────────────┐
 │  Creaves A   │ ──────────────────────► │                  │
 │  (database)  │                         │  Creaves Console │──► Dashboard
 └──────────────┘                         │  (database)      │──► Reports
 ┌──────────────┐   webhook (HTTP POST)   │                  │──► Drill-down
 │  Creaves B   │ ──────────────────────► │                  │
 │  (database)  │                         └──────────────────┘
 └──────────────┘
```

- **Creaves (main)**: PUSH side — produces events and sends them to the console.
- **Creaves Console**: RECEIVE side — accepts events, processes them into a
  consolidated view.

The two apps share **no code and no database**. They communicate exclusively through the
webhook HTTP contract documented below.

---

## Documentation Index

| Document | Purpose |
|----------|---------|
| **AGENTS.md** (this file) | Architecture, setup, webhook contract, testing |
| **TODO.md** | Implementation plan & status |
| `migrations/schema.sql` | Full database schema dump |

---

## Tech Stack

| Layer | Tech |
|-------|------|
| Language | Go 1.21 (`go.mod`) |
| Web framework | [Buffalo](https://gobuffalo.io) |
| ORM / DB | [Pop v6](https://github.com/gobuffalo/pop) + MySQL/MariaDB (dev/prod), SQLite (test) |
| Templating | Plush `.plush.html` (embedded via `go:embed`) |
| Auth | Custom session-based, bcrypt password hashes |
| API auth | Bearer token (bcrypt-hashed webhook API keys) |

---

## Prerequisites

- Go 1.21+
- Buffalo CLI: `go install github.com/gobuffalo/cli/cmd/buffalo@latest`
- MySQL/MariaDB (dev/prod); SQLite with CGO for tests
- CGO and SQLite driver for tests (see Testing section)

## Database Setup

Config: `database.yml`

```yaml
development:
  dialect: "mysql"
  database: "consolidation"
  host: "localhost"
  port: "3306"
  user: "creaves"
  password: "creaves"
```

Production uses env vars: `DB_NAME`, `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`.

```bash
# Run migrations
buffalo pop migrate up

# Create admin user (login=admin, password=admin123)
buffalo task db:seed
```

---

## Development Commands

```bash
# Build & run dev server (port 3001 by default)
buffalo dev

# Build production binary
buffalo build --environment production -o bin/creaves-console

# Run migrations
buffalo pop migrate up

# Seed admin user
buffalo task db:seed

# Grift tasks
buffalo task consolidation:process   # Process unprocessed events
buffalo task consolidation:rebuild   # Rebuild consolidated view from scratch
buffalo task consolidation:stats     # Show statistics
CONFIRM=cleanup buffalo task db:cleanup  # Delete application data; preserves migrations
```

---

## Architecture

### Key Files

| File | Purpose |
|------|---------|
| `actions/app.go` | All routes + middleware |
| `actions/webhook.go` | **Webhook receiver** — accepts events from Creaves instances |
| `actions/instances.go` | Registered instance listing and transactional cleanup |
| `actions/webhook_resync_handlers.go` | Resync start/status admin endpoints |
| `actions/webhook_api_keys.go` | CRUD for webhook API keys (admin UI) |
| `actions/event_processor.go` | Processes events into `consolidated_animals` |
| `actions/consolidation_runner.go` | Orchestrates processing workflow |
| `actions/sync_checksum.go` | Per-instance sync status (expected/confirmed/unconfirmed + shared state-set checksums) for the sync-management view |
| `actions/dashboard.go` | Dashboard, consolidated animal list, drill-down, reports |
| `actions/users.go` | Auth (session), user CRUD |
| `actions/render.go` | Render engine, helpers |
| `models/event_stream.go` | Event model + payload structs |
| `actions/events.go` | Admin event stream browser (index/show) |
| `actions/events_delete.go` | Admin deletion of received events (all / per instance) with JSONL archive |
| `models/consolidated_animal.go` | Consolidated view model + `ApplyEvent()` logic |
| `models/webhook_api_key.go` | API key model + `GenerateKey()` / `Authenticate()` |
| `models/import_run.go` | Import/processing run tracking |
| `models/user.go` | User model |

### Data Flow (Webhook Event → Consolidated View)

```
1. Creaves instance creates an event (animal discovered, status changed, released, died)
2. Creaves webhook pusher (webhook_pusher.go) POSTs event batch to Console
3. Console WebhookEventsHandler (webhook.go):
   a. Authenticates via Bearer token → looks up API key (bcrypt compare)
   b. Validates instance_id against key (if key is instance-restricted)
   c. For each event: checks idempotency (UUID), creates EventStream record
   d. Processes event immediately (synchronous) via EventProcessor
4. EventProcessor (event_processor.go):
   a. Finds-or-creates ConsolidatedAnimal by (instance_id, animal_id)
   b. Applies the event payload to update the consolidated record
   c. Marks event as processed
5. Dashboard/reports read from consolidated_animals
```

### Routes (app.go)

| Method | Path | Handler | Auth |
|--------|------|---------|------|
| POST | `/webhook/events` | `WebhookEventsHandler` | **Bearer token** (API key) |
| GET | `/instances` | `InstancesIndex` | Session |
| POST | `/instances/:instance_id/cleanup` | `InstanceCleanup` | Admin |
| GET | `/events` | `EventsIndex` | Admin |
| GET | `/events/delete` | `EventsDeleteNew` | Admin |
| POST | `/events/delete` | `EventsDeleteCreate` | Admin |
| GET | `/events/:event_id` | `EventShow` | Admin |
| GET/POST | `/webhook_resync`, `/webhook_resync/start` | Resync handlers | Admin |
| GET | `/webhook_resync/status.json` | `WebhookResyncStatus` | Session |
| GET | `/` | `DashboardIndex` | Session |
| GET | `/dashboard` | `DashboardIndex` | Session |
| GET | `/auth/new` | `AuthNew` | None |
| POST | `/auth` | `AuthCreate` | None |
| GET/POST/etc | `/users` | `UsersResource` | Admin |
| GET/POST/etc | `/webhook_api_keys` | `WebhookAPIKeysResource` | Admin |
| GET | `/consolidated_animals` | `ConsolidatedAnimalsIndex` | Session |
| GET | `/consolidated_animals/:id` | `ConsolidatedAnimalShow` | Session |
| GET | `/consolidated_animals/:id/drill_down` | `ConsolidatedAnimalDrillDown` | Session |
| GET | `/reports` | `ReportsIndex` | Session |
| GET | `/reports/by_location` | `ReportsByLocation` | Session |
| GET | `/reports/by_type` | `ReportsByType` | Session |
| GET | `/reports/by_species` | `ReportsBySpecies` | Session |
| GET | `/reports/annual` | `ReportsAnnualIndex` | Session |
| GET | `/reports/annual/export.csv` | `ReportsAnnualExportCSV` | Session |

### Middleware Stack

```
paramlogger → csrf → popmw.Transaction(models.DB) → i18n
```

**Important**: The `/webhook/events` route **skips CSRF** (it's a machine-to-machine API
call). All other routes use session auth + CSRF.

---

## Webhook Contract

This is the HTTP contract between Creaves (main) and Creaves Console.

**Canonical values**: payload display values remain canonical base-locale (French) reference names. Sync v2 additionally accepts an optional `translations` map in each payload and stores it for localized display; matching/report grouping continues to use canonical values.

### Endpoint

```
POST /webhook/events
```

### Authentication

```
Authorization: Bearer creaves_<uuid>
```

The key is an API key generated in the Console admin UI (`/webhook_api_keys/new`).
Keys are stored as **bcrypt hashes** — the raw key is shown only once on creation.

### Request Body

```json
{
  "events": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "instance_id": "center-north",
      "animal_id": 42,
      "event_type": "animal_discovered",
      "payload": { ... },
      "created_at": "2024-01-15T10:30:00Z"
    }
  ]
}
```

### Event Types

| Type | When |
|------|------|
| `animal_discovered` | New animal intake/discovery |
| `animal_status_changed` | Status update (e.g. in_care → under_treatment) |
| `animal_released` | Animal released back to wild |
| `animal_died` | Animal died in care |
| `animal_state` | Full current-state resync snapshot (replace semantics) |

### Payload Structure (the `payload` field)

```json
{
  "animal": {
    "id": 42, "year": 2024, "year_number": 17,
    "species": "Hérisson", "gender": "M", "cage": "A12",
    "zone": "Quarantine", "ring": "FR-2024-017",
    "animal_type": "Mammifère", "animal_age": "Adulte",
    "species_class": "Mammalia", "species_agw_group": "...",
    "species_subside_group": "...", "species_native_status": "Indigène"
  },
  "discovery": {
    "id": "uuid", "location": "...", "postal_code": "67000",
    "city": "Strasbourg", "date": "2024/01/15 10:30",
    "entry_cause": "...", "entry_cause_detail": "...",
    "entry_cause_nature": "...", "reason": "...", "note": "...",
    "return_habitat": false, "in_garden": true,
    "discoverer_firstname": "...", "discoverer_lastname": "...",
    "discoverer_address": "...", "discoverer_city": "...",
    "discoverer_postal_code": "...", "discoverer_country": "...",
    "discoverer_email": "...", "discoverer_phone": "...",
    "discoverer_note": "..."
  },
  "intake": {
    "id": "uuid", "date": "2024/01/15 11:00",
    "general": "...", "has_wounds": true, "wounds": "...",
    "has_parasites": false, "parasites": "...", "remarks": "..."
  },
  "outtake": {
    "id": "uuid", "date": "2024/03/01 09:00",
    "type": "Released to Wild", "location": "...", "note": "...",
    "rating": 1, "dead": false
  },
  "initial_status": "in_care",
  "current_status": "in_care",
  "previous_status": "",
  "user_id": "uuid", "user_login": "admin",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Response

```json
{
  "processed": 5,
  "total": 5
}
```

On partial failure (some events errored):

```json
{
  "processed": 3,
  "total": 5,
  "errors": ["failed to process event ...: ..."]
}
```

The Creaves pusher marks the batch as delivered when it receives HTTP 200.

### Idempotency

- Events are deduplicated by **UUID** (`id` field).
- If an event with the same UUID already exists, it's counted as success (skipped).
- Safe to replay the same batch multiple times.

---

## Database Schema

### `event_streams` (received events)

| Column | Type | Notes |
|--------|------|-------|
| `id` | char(36) PK | UUID — same as source event |
| `instance_id` | varchar(255) | Source instance identifier |
| `animal_id` | int | Source animal ID |
| `event_type` | varchar(255) | See event types above |
| `payload` | json | Full structured payload |
| `source_db` | varchar(255) | Deprecated (legacy from pull model) |
| `imported_at` | datetime | When received via webhook |
| `processed_at` | datetime NULL | When processed into consolidated view |
| `created_at` | datetime | Event creation time from source |
| `updated_at` | datetime | |

Index: `(instance_id, animal_id, created_at)`, `processed_at`

### `consolidated_animals` (the unified view)

| Column | Type | Notes |
|--------|------|-------|
| `id` | char(36) PK | Generated UUID |
| `instance_id` + `animal_id` | UNIQUE | Composite uniqueness per source animal |
| `year`, `year_number` | int | Animal identification |
| `species`, `gender`, `cage`, `zone`, `ring` | varchar NULL | |
| `species_class`, `species_agw_group`, `species_subside_group`, `species_native_status` | varchar NULL | Species taxonomy from source species table |
| `animal_type`, `animal_age` | varchar NULL | |
| `discovery_location`, `discovery_date`, `discovery_city`, `discovery_postal_code` | | |
| `entry_cause` | varchar NULL | |
| `entry_cause_detail`, `entry_cause_nature` | varchar NULL | |
| `current_status` | varchar NOT NULL | in_care / under_treatment / released / died |
| `intake_date`, `intake_general`, `intake_wounds`, `intake_parasites`, `intake_remarks` | | |
| `outtake_date`, `outtake_type`, `outtake_location` | | |
| `outtake_rating` | int NULL | From outtake type definition |
| `outtake_dead` | bool NULL | From outtake type definition |
| `last_event_at` | datetime | |
| `event_count` | int | Number of events applied |
| `created_at`, `updated_at` | datetime | |

**Unique constraint**: `consolidated_animals_instance_id_animal_id_idx (instance_id, animal_id)`

### `webhook_api_keys`

| Column | Type | Notes |
|--------|------|-------|
| `id` | char(36) PK | |
| `name` | varchar | Human-readable label |
| `key_hash` | varchar | bcrypt hash of the raw key |
| `key_prefix` | varchar | First 8 chars of UUID (for identification) |
| `instance_id` | varchar NULL | Optional: restrict key to one instance |
| `active` | bool | |
| `last_used_at` | datetime NULL | |

### `import_runs`

Tracks each processing run (status, events processed, errors).

---

## Setup Guide (Console Administrator)

### 1. Deploy the Console

```bash
buffalo pop migrate up
buffalo task db:seed
buffalo dev   # or buffalo build + deploy binary
```

### 2. Create a Webhook API Key

1. Login as `admin` / `admin123`
2. Go to **Webhook API Keys** → **New**
3. Enter a name (e.g. "Center North")
4. Optionally restrict to a specific `instance_id`
5. **Copy the raw key immediately** — it's only shown once (`creaves_<uuid>`)

### 3. Configure Each Creaves Instance

On each Creaves instance, go to the **Configuration** page and set:
- **Enable Webhook**: checked
- **Webhook URL**: `https://<console-host>/webhook/events`
- **API Key**: the raw key from step 2
- **Batch Size**: 1 (increase for throughput)
- **Max Events Per Minute**: 60 (adjust to your needs)

### 4. Verify

- Create/edit an animal on a Creaves instance
- Check the Console dashboard — the event should appear within seconds
- Use `buffalo task consolidation:stats` to verify counts

---

## Testing

### Unit Tests

Tests use **SQLite** (in-memory/file-based). Requires CGO and the SQLite build tag:

```bash
# IMPORTANT: SQLite requires CGO + the sqlite build tag
CGO_ENABLED=1 go test -tags sqlite ./actions/... ./models/...
```

Without `-tags sqlite`, tests will fail with:
`sqlite3 support was not compiled into the binary`

### Existing Tests

| File | Covers |
|------|--------|
| `actions/event_processor_test.go` | Event processing, idempotency, multi-instance, status transitions, stats, run history |
| `actions/event_processor_poison_test.go` | Regression: a poison event must not block newer events during replay |
| `actions/sync_checksum_test.go` | Shared checksum golden vectors + per-instance counts math |
| `actions/webhook_e2e_test.go` | Full push→receive→process flow over the real handler (contract E2E) |
| `actions/webhook_e2e_second_extract_test.go` | E2E: second full extract keeps all years incl. current year, with a poison event present |
| `models/models_test.go` | Model validation tests |

### Test Database

The test setup (`TestMain` in `event_processor_test.go`) creates a SQLite database
with all tables manually, cleans tables before each test, and tears down on exit.

### What to Test When Adding Features

- **New event types**: Add a test case in status transition tests
- **Webhook receiver changes**: Test authentication, idempotency, error handling
- **New payload fields**: Test that `UpdateFromPayload` correctly maps them

---

## Docker

```bash
# Build and run
docker-compose up -d

# Run migrations inside container
docker-compose exec consolidation-app ./consolidation migrate

# Run consolidation task
docker-compose run --rm consolidation-cli process
```

`Dockerfile` is a multi-stage build. `docker-compose.yml` includes MySQL + app + cron.

---

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Events not appearing in console | 1. Is webhook enabled on Creaves instance? 2. Is API key correct? 3. Is console reachable from Creaves network? 4. Check Creaves logs for delivery errors |
| `sqlite3 support was not compiled` | Run tests with `CGO_ENABLED=1 -tags sqlite` |
| 401 Unauthorized on webhook | API key mismatch — regenerate key, ensure bcrypt compare works |
| Duplicate events | Not possible — UUID dedup. Check if same UUID is sent twice (shouldn't be) |
| Consolidated view stale | Run `buffalo task consolidation:process` to process pending events |
| Need full rebuild | `buffalo task consolidation:rebuild` — clears consolidated_animals and reprocesses all events |

---

## Key Design Decisions

1. **Webhook push (not pull)**: Creaves databases are geographically spread.
   Direct DB connections from console to each instance are not feasible (firewalls,
   NAT, network topology). Push via HTTP is the only reliable sync method.

2. **Separate database**: Console has its own `consolidation` database. No shared
   tables with any Creaves instance. This keeps the console fully independent.

3. **Synchronous processing**: Events are processed immediately on receipt (in the
   webhook handler). This keeps the architecture simple. For high volume, an
   async queue could be added later.

4. **Idempotent by design**: UUID-based dedup means retries are safe. The pusher
   can resend the same batch without creating duplicates.

5. **Legacy pull model removed**: The old `source_instances` table (pull-based import)
   has been dropped. All sync is now push-based via webhooks. See migration
   `20260424210001_drop_source_instances`.

---

## Current Status / Known Issues

- **WIP**: The webhook system is functional but needs hardening:
  - Pusher worker on Creaves side is not started at boot (only starts lazily when
    first event is published — see Creaves `webhook_pusher.go`)
  - Console tests require SQLite build tag (documented above)
- **Deprecated fields**: `source_db` in event_streams is leftover from the old pull
  model; always empty now.
- **Sync visibility (phase 8, done)**: `/sync_management` shows per-instance
  expected/confirmed/unconfirmed counts + event-log vs consolidated checksums;
  replay (`ProcessUnprocessedEvents`) skips-and-continues on poison events
  (see `docs/archive/2026-09-05-phase-8-sync-visibility.md`).

See `TODO.md` for the implementation plan and status.