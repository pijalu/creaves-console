# Consolidation Application - Implementation Plan

## Sync v2 rollout status

Console-first Sync v2 implementation validated through T7.3; deployment-specific manual cross-project checks are documented in `docs/plan/subplans/phase-7-e2e-docs.md`.

## Overview
The consolidation application must run as a standalone CLI tool that can be executed independently of the Buffalo web server. It should support cron-based periodic execution, incremental imports (remembering last import time), and idempotent processing.

## Architecture

### 1. CLI Mode (No Buffalo Required)
- Create a `cmd/cli` package with a pure CLI entry point
- CLI connects directly to the database using pop.Connection
- No HTTP server, no middleware, no sessions
- Can be run via: `./consolidation-cli import` or `docker run consolidation-cli import`

### 2. Delta/Incremental Import
- SourceInstance.LastImport tracks last successful import timestamp
- Import only fetches events with created_at > LastImport
- Events are deduplicated by UUID (idempotent)
- Import is atomic within a transaction

### 3. Idempotent Processing
- Process only events where processed_at IS NULL
- ConsolidatedAnimal upsert is idempotent (same event applied twice = same result)
- Event UUID is unique constraint prevents duplicates
- Can safely rerun import+process multiple times

### 4. Docker Support
- Single binary that supports both `serve` and `import` subcommands
- Dockerfile creates lightweight image
- Docker compose includes cron service
- Special docker run command for one-off imports

## Implementation Tasks

### TODO-001: Create CLI Entry Point
- Create `cmd/cli/main.go` with subcommands: `import`, `process`, `rebuild`, `stats`
- Initialize DB connection without Buffalo app
- Parse command-line flags

### TODO-002: Refactor Import for CLI
- Extract import logic from HTTP handlers into pure functions
- Create `ImportService` that works with pop.Connection directly
- Support importing from all sources or specific source

### TODO-003: Add Import Run Tracking
- Create `import_runs` table to track each import execution
- Fields: id, started_at, completed_at, source_count, events_imported, events_processed, status, error_message
- Update LastImport on SourceInstance only after successful import

### TODO-004: Docker Cron Setup
- Create docker-compose with cron service
- Cron runs: `docker run --rm consolidation-cli import` every X minutes
- Or use Kubernetes CronJob for container orchestration

### TODO-005: Connection String Fix
- Fix SourceInstance.ConnectionString() to use strconv.Itoa for port
- Current implementation has bug with rune conversion

### TODO-006: Unit Tests with SQLite
- Create test suite using SQLite in-memory database
- Test import from source to target
- Test idempotent processing
- Test delta import (only new events)
- Drop test DB on teardown

### TODO-007: Consolidation Runner
- Create `ConsolidationRunner` that orchestrates import+process
- Support dry-run mode
- Support force-full-reimport mode
- Logging and metrics

### TODO-008: Error Handling & Recovery
- Failed imports should not update LastImport
- Partial failures should be logged in import_runs
- Support retry logic for transient DB errors

## Usage Examples

### CLI Commands
```bash
# Import and process all sources
./consolidation-cli import

# Process only (no import)
./consolidation-cli process

# Rebuild everything
./consolidation-cli rebuild

# Show stats
./consolidation-cli stats

# Import from specific source
./consolidation-cli import --source=source-id

# Dry run
./consolidation-cli import --dry-run
```

### Docker
```bash
# One-off import
docker run --rm -e DATABASE_URL=... consolidation-cli import

# With docker-compose
docker-compose run --rm consolidation-cli import

# Cron (every 5 minutes)
*/5 * * * * docker run --rm -e DATABASE_URL=... consolidation-cli import
```

## Database Schema Additions

### import_runs table
```sql
CREATE TABLE import_runs (
    id UUID PRIMARY KEY,
    started_at TIMESTAMP NOT NULL,
    completed_at TIMESTAMP,
    source_count INT DEFAULT 0,
    events_imported INT DEFAULT 0,
    events_processed INT DEFAULT 0,
    status VARCHAR(20) DEFAULT 'running',
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

## Testing Strategy

### Unit Tests
- Use SQLite in-memory for speed
- Create source DB with test events
- Import and verify events in target
- Test delta import (add new events, reimport, verify only new ones imported)
- Test idempotent processing (process same events twice, verify same result)
- Test error handling (simulate source DB failure)

### Integration Tests
- Test with real MySQL source and target
- Test full import+process cycle
- Test concurrent imports

## Success Criteria
- [x] CLI can import without running Buffalo server
- [x] Import remembers last import time (delta only)
- [x] Can rerun import safely (idempotent)
- [x] Docker image supports both serve and import modes
- [x] Unit tests pass with SQLite
- [ ] Integration tests pass with MySQL
- [x] Failed imports don't corrupt state

## Implementation Status

All core features have been implemented:

1. **CLI Tool** (`cmd/cli/main.go`): Standalone binary with subcommands
2. **Import Tracking** (`models/import_run.go`): Tracks every execution
3. **Delta Import** (`actions/event_importer.go`): Uses LastImport timestamp
4. **Idempotent Processing** (`actions/event_processor.go`): Safe to rerun
5. **Docker Support** (`Dockerfile`, `docker-compose.yml`): Multi-stage build with cron
6. **Unit Tests** (`actions/event_processor_test.go`): 8 tests with SQLite
7. **Consolidation Runner** (`actions/consolidation_runner.go`): Orchestrates workflow
8. **Connection String Fix** (`models/source_instance.go`): Uses fmt.Sprintf

## Running Tests

```bash
# Build with SQLite support
cd consolidation
CGO_ENABLED=1 go test -tags sqlite ./...

# Run CLI
./consolidation-cli import
./consolidation-cli process
./consolidation-cli stats

# Docker
docker-compose up -d
docker-compose run --rm consolidation-cli import
```