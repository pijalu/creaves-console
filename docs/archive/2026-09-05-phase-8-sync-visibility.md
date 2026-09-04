# Phase 8 — Sync Visibility: Expected Counts + Checksum Validation + 2026 Sync Bug

Status: **DONE** (2026-09-05) — implemented in both projects; see commits in creaves-console and creaves
Date: 2026-09-05
Scope: **both** projects — creaves (producer) + creaves-console (receiver)

## 1. Problem statements

### 1.1 Feature: admin sync visibility

Admins have no way to answer "is my console in sync with this creaves instance?"
The existing views are incomplete:

- creaves `/webhook_resync`: resync run progress only — no expected-vs-confirmed
  record counts, no per-year breakdown, no content fingerprint.
- console `/sync_management`: stored-animal counts per instance only — no
  *expected* counts derived from the received event log, no confirmation status,
  no checksum.

Because creaves sits behind NAT and cannot be queried by the console (and vice
versa), verification must work by **comparing two independently computed
fingerprints** shown in the two admin UIs.

### 1.2 Bug: "no animals of 2026 after full extract"

After a full extract (full resync / rebuild), the console shows no animals of the
current year. Review findings:

- **F1 (fixed here): poison-event queue block.**
  `EventProcessor.ProcessUnprocessedEvents` (creaves-console) iterates events
  `ORDER BY created_at ASC` and **returns on the first processing error**.
  One malformed/legacy event permanently blocks every *newer* event from being
  (re)processed during a full replay. Newest events = current-season (2026)
  animals ⇒ exactly the reported symptom shape. The live webhook handler already
  continues per-event on errors; the replay path must do the same
  (skip + collect + surface, never abort the tail).
- **F2 (mitigated here): invisibility.** Whatever the original trigger
  (poison event, failed payload build on the producer, partial batch), no admin
  surface showed *which year/instance* lost records. The per-instance
  confirmed/unconfirmed counts + per-year breakdown + checksums below make the
  breakage point visible and verifiable after any future incident.

## 2. Shared checksum definition (contract addition, both projects)

```
state_set_checksum(records) = "sha256:" + hex( sha256( join("\n", lines) ) )
line = "<animal_id>|<state_hash>"
records sorted lexicographically by line
```

- `state_hash` = the v2 content hash (`resync_hash.go` on creaves; `state_hash`
  column / `payload.state_hash` on console).
- creaves computes the **expected** checksum over its *current animals*
  (state hash recomputed live per animal).
- console computes **two** checksums per instance:
  - *event-log checksum*: over `event_streams` rows with
    `event_type='animal_state'` (payload `state_hash`),
  - *consolidated checksum*: over `consolidated_animals.state_hash` rows.
- Interpretation: expected == event-log ⇒ producer→console transfer complete;
  event-log == consolidated ⇒ console processing complete; all three equal ⇒
  verified sync. Empty set ⇒ `sha256:e3b0c442...` (empty-input SHA-256).

## 3. Console changes (`creaves-console`)

1. `actions/sync_checksum.go` — checksum + per-instance count queries:
   - expected total: `COUNT(DISTINCT animal_id)` in event log for instance
     (any event type),
   - confirmed: consolidated rows whose `state_hash` equals the latest state
     event hash for that animal,
   - unconfirmed: expected − confirmed,
   - both checksums.
2. `sync_management/index` gains columns: Expected / Confirmed / Unconfirmed /
   checksum match badge, plus orphan rows keep working. Per-year counts for the
   instance shown in a details table (the "no 2026" visibility).
3. `EventProcessor.ProcessUnprocessedEvents`: skip-and-continue on per-event
   errors, collect them, return count + errors; callers log. Poison events can
   no longer orphan newer data (F1).
4. Tests (SQLite): checksum golden vectors; counts math (mixed years incl. 2026,
   missing consolidated row, hash mismatch, orphan); replay-survives-poison
   regression; template render smoke.

## 4. Creaves changes (`creaves`)

1. `actions/sync_status.go` — expected-set computation for the instance:
   per-animal state hash (reuse `StateContentHashPayload`), counts:
   total animals, state-confirmed (delivered state event with current hash),
   state-unconfirmed (pending/undelivered), never-synced (no state event),
   expected checksum, per-year breakdown.
2. `/webhook_resync` view (status panel + when idle) shows these numbers.
3. Tests (MySQL): counts/checksum on seeded fixtures incl. year 2026 rows.

## 5. Tasks

| # | Task | Project |
|---|------|---------|
| 1 | sync checksum helper + golden tests | both |
| 2 | console counts/badge/year table + tests | console |
| 3 | replay poison-event fix + regression | console |
| 4 | creaves expected-set status + view + tests | creaves |
| 5 | e2e: second full extract keeps all years | console |
| 6 | validation (test suites, vet, staticcheck) | both |
| 7 | archive plan, AGENTS.md updates, commit | both |

## 6. Validation gates

- console: `CGO_ENABLED=1 go test -tags sqlite -count=1 ./...`, `go vet`, `staticcheck`
- creaves: `go test ./actions/... ./models/...` (MySQL test DB), `go vet`, `staticcheck`
- e2e second-extract test passes incl. 2026 fixtures
