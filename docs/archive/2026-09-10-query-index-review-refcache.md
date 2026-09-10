# creaves-console: query/index review — landing & register page optimization + change-driven cache

Closed: 2026-09-10. Commits: `1e389b3` (indexes), `b7de937` (refcache), `2935278` (auth prefilter).

## Observed (review, live dev DB: 10,121 consolidated_animals / 6,539 event_streams)

- `/consolidated_animals` (register) ran 12 aux queries per render: 7 `SELECT DISTINCT`
  dropdown queries + 5 `localizedGroupLabels` GROUP BY queries incl. `MIN(translations)`
  JSON scan (actions/dashboard.go). `entry_cause`, `animal_age`, `outtake_type` had NO
  index → EXPLAIN `type=ALL` + temporary + filesort.
- `EventsIndex` ordered `imported_at desc` (actions/events.go) with no index → full scan +
  filesort, growing unbounded.
- Webhook auth (actions/webhook.go `findAndAuthenticateKey`) bcrypt-compared every active
  key per request (~60-100ms each); `key_prefix` unused for lookup.
- Landing page (`DashboardIndex`) verified index-covered (EXPLAIN: "Using index",
  "Using index for group-by") — no change needed.
- Migrations ↔ `migrations/schema.sql` dump: no drift.

## Fix

1. Migration `20260910120000_add_query_optimization_indexes` (additive):
   `consolidated_animals(entry_cause)`, `(animal_age)`, `(outtake_type)`,
   `event_streams(imported_at)`.
2. `actions/refcache.go` — per-dimension value-set + localized-label cache,
   invalidated by data change (not TTL):
   - tracked fields: species, animal_type, animal_age, entry_cause, outtake_type,
     discovery_city, year;
   - `EventProcessor.processEvent` observes each processed payload — a value unknown to
     a warm cache drops the field entry; cold fields skipped (resync replays don't thrash);
   - `InstanceCleanup`, events deletion, `ProcessAllEvents` (rebuild) invalidate all;
   - single-flight rebuilds under write lock; build errors returned uncached;
   - in-process by design (single-container deploy); multi-replica needs a shared
     invalidation signal first.
3. Register handler serves dropdowns/labels via cache; templates (base/fr/nl/de) iterate
   plain strings.
4. Webhook auth: `key_prefix` prefilter before bcrypt.

## Validation

- Migration applied to dev DB; EXPLAIN after: 3 DISTINCT queries "Using index for
  group-by", events browser "Backward index scan" — no filesort.
- Unit tests `actions/refcache_test.go`: caching, per-scope entries, error-uncached,
  unknown-value invalidation, InvalidateAll/Reset, single-flight, concurrency.
- Full suite: `CGO_ENABLED=1 go test -tags sqlite -count=1 ./actions/... ./models/... ./excel/...` ok;
  `go vet`, `staticcheck` clean; `gocognit -over 15` / `gocyclo -over 12`: only
  pre-existing unrelated findings (UpdateFromPayload 59/58, installSafePopTxLogger 25/13,
  LocalizedField 21, animal_sort_test 23, SyncManagementIndex 16/14,
  applyConsolidatedAnimalFilters 14, UpsertByInstanceID 13, EventsDeleteCreate 13 —
  identical at HEAD);
  `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...` ok
  (56.4% / 64.5% / 59.4%).
- E2E (agent-browser, buffalo dev + MySQL, admin login):
  - landing `/`: 10,121 total animals/events, status table rendered;
  - `/consolidated_animals`: dropdowns populated (species 172, age 4, entry_cause 26,
    outtake_type 8, year 7, city 624 options);
  - warm reload: 0 new `SELECT DISTINCT … as v` / `MIN(translations)` queries in dev log;
  - nl locale register renders (172 options, no template errors); 0 ERRO/500 in log.
