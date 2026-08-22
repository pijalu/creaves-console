# Creaves ↔ Creaves Console Sync Specification — v2

Status: **DRAFT — approved for implementation planning**
Owner project: **creaves-console** (main project for sync)
Companion document: [`docs/plan/SYNC_IMPLEMENTATION_PLAN.md`](./docs/plan/SYNC_IMPLEMENTATION_PLAN.md) — review findings + TDD micro-tasks (per-phase subplans in [`docs/plan/subplans/`](./docs/plan/subplans/))
Supersedes: the "Webhook Contract" sections of both `AGENTS.md` files (those remain authoritative for v1 until this spec ships).

---

## 1. Purpose

Creaves Console consolidates data from multiple geographically-distributed Creaves
instances via a one-way webhook push. This specification (contract **v2**) adds:

| ID | Requirement | Source |
|----|-------------|--------|
| R1 | Console must allow an administrator to **clean up (purge) an instance easily** — one action deletes everything belonging to that instance | user |
| R2 | Console must have a first-class **instance entity**; **all reporting** must support a **global OR per-instance view** | user |
| R3 | Creaves must have a **full resync** that sends *all* content (all animals, current contract entities) to the console, started **in background**, with a **web view showing real-time progress** | user |
| R4 | **All sync calls must be idempotent** (batch, resync, cleanup, instance registration) | user |
| R5 | Console is **multi-lingual**; therefore **every event pushed by creaves must contain all languages** for reference-data fields | user |
| R6 | Console UI itself gains the missing **French** locale (currently en-US / de / nl only) | derived from R5 |

Non-goals for v2 (may be revisited later):

- Pushing full care/treatment/veterinary-visit history (decided: out of scope; the
  resync sends the **current contract entities** for every animal — see §6.1).
- Bi-directional sync (console → creaves commands).
- Async event queue on the console (processing stays synchronous).

---

## 2. Roles and communication

Unchanged from v1: creaves is the PUSH side, console the RECEIVE side, the only
channel is `POST /webhook/events` with Bearer API-key auth. No shared code, no
shared database. Both projects define the wire structs independently; this spec is
the single source of truth for the shared shapes.

---

## 3. Wire contract v2

### 3.1 Endpoint & auth (unchanged)

```
POST /webhook/events
Content-Type: application/json
Authorization: Bearer creaves_<uuid>
```

Response on success / partial failure (unchanged shape):

```json
{ "processed": 3, "total": 5, "processed_ids": ["..."], "errors": ["..."] }
```

Rules kept from v1:

- Per-event dedup by **UUID**; a duplicate UUID already present **and processed** is
  counted as success without side effects.
- A duplicate UUID that exists but is **unprocessed** is reprocessed (self-healing redelivery).
- Events failing validation are reported in `errors` and **not** counted in `processed_ids`,
  so the creaves pusher leaves them undelivered and retries them.

### 3.2 Envelope additions (backward compatible)

```json
{
  "contract_version": 2,
  "instance": {
    "id": "center-strasbourg",
    "name": "Centre Strasbourg",
    "description": "..."
  },
  "events": [ ... ]
}
```

- `contract_version` — optional integer, absent in v1. Console uses it for logging only;
  feature detection is per-field (missing = absent).
- `instance` — optional object. On every webhook call that carries it, the console
  **upserts** the instance registry row (§4.1) keyed by `instance.id`: sets/updates
  `name`/`description` and `last_seen_at`. Never fails the batch on upsert error —
  the error is logged and the batch proceeds.
- `instance.id` **must** equal each event's `instance_id` (console validates; mismatch →
  that event lands in `errors` exactly like a restricted-key mismatch today).
- v1 senders (no `instance`, no `contract_version`) remain fully supported; the console
  auto-registers the instance lazily from the first event (§4.1).

### 3.3 Event types

| Type | Trigger | Semantics on console |
|------|---------|----------------------|
| `animal_discovered` | new intake | merge (v1 behavior) |
| `animal_status_changed` | status update | merge (v1 behavior) |
| `animal_released` | release outtake | merge, status := released (v1) |
| `animal_died` | death outtake | merge, status := died (v1) |
| **`animal_state`** *(new)* | full resync snapshot | **replace** (§6.3) |

`animal_state` carries the **complete current state** of one animal under the current
contract: `animal`, `discovery`, `intake`, `outtake` (when present), `current_status`,
`initial_status` (`current_status` mirrored), plus `translations` (§3.4).

### 3.4 Multilingual payload (R5)

The event `payload` gains an optional `translations` object containing, for **every
locale the producer supports**, the localized display values of all
**reference-data fields** present in the payload:

```json
"translations": {
  "fr":    { "species": "Hérisson", "animal_type": "Mammifère", "animal_age": "Adulte",
             "zone": "Quarantaine", "outtake_type": "Relâché en nature", "entry_cause": "..." },
  "en-US": { "species": "Hedgehog", "animal_type": "Mammal",    "animal_age": "Adult",
             "zone": "Quarantine",  "outtake_type": "Released to Wild", "entry_cause": "..." },
  "de":    { ... },
  "nl":    { ... }
}
```

Rules:

- **Locales**: exactly the producer's `models.SupportedLocales` = `fr`, `en-US`, `de`, `nl`.
  The producer includes a locale key only when a non-empty translation exists for it in its
  `translations` table; when a specific field has no translation, the field key is omitted
  from that locale's map. The console falls back to the canonical (French) base value
  carried in the payload itself.
- **Translatable fields** (closed set, extensible by spec amendment only):
  `species`, `animal_type`, `animal_age`, `zone`, `outtake_type`, `entry_cause`.
  Free user content (cage, ring, city, notes, discoverer names) and coded values
  (`gender`) are **not** translated.
- `fr` is included explicitly (same value as canonical base) so consumers need no
  hardcoded base-locale knowledge.
- Console storage: the latest non-empty `translations` object received for an animal is
  stored verbatim as JSON on `consolidated_animals.translations` (§4.2) and used for
  localized rendering. Grouping/aggregation in reports always groups by **canonical**
  value; only the displayed label is localized.

### 3.5 Idempotency model (R4)

Three layers, all mandatory:

1. **Envelope UUID dedup** (v1, kept): identical event delivered twice ⇒ second is a no-op success.
2. **Content-addressed state events** (new): for `animal_state` events the producer derives
   the event UUID **deterministically from the content**:
   - `content_hash = sha256( canonical_content_string )` where `canonical_content_string` is
     the `\x1f`-joined concatenation of exactly these payload fields, in this order, empty
     strings used when absent:
     `instance_id, animal_id, year, year_number, species, gender, cage, zone, ring,
     animal_type, animal_age, current_status, discovery.location, discovery.postal_code,
     discovery.city, discovery.date, discovery.entry_cause, discovery.reason,
     intake.date, intake.general, intake.wounds, intake.parasites, intake.remarks,
     outtake.date, outtake.type, outtake.location, translations_json_sorted`
     (`translations_json_sorted` = the `translations` object serialized with
     lexicographically sorted keys; `timestamp`/`user_*` fields are **excluded** — they are volatile).
   - `event_uuid = uuid.NewV5(NAMESPACE_CREAVES_STATE, instance_id + "|" + animal_id + "|" + hex(content_hash))`
   - `NAMESPACE_CREAVES_STATE = 9f6d3e20-7c1a-4b8f-9e2a-3d4c5b6a7f10` (fixed, shared literal in both projects).
   - Consequences: unchanged animal re-resynced ⇒ identical UUID ⇒ console dedup-skips
     (true no-op). Changed animal ⇒ new UUID ⇒ processed as a state replace.
3. **Producer-side skip**: before creating a resync event, creaves checks its own
   `event_streams` for an existing `animal_state` row for this animal with the same
   `content_hash` (new indexed column, §6.2). If present and delivered, the event is not
   recreated at all (saves queue/DB bloat and console round-trips). If present but
   undelivered, it is left in the queue (already pending delivery).

Additional idempotency guarantees:

- **Resync start**: at most one active resync run per instance (DB-enforced); starting a
  second returns HTTP 409. Re-clicking start after completion is a fresh run and is
  content-idempotent thanks to layer 2.
- **Console cleanup**: `DELETE-everything-for-instance` is idempotent by construction
  (second run deletes 0 rows); it runs in a single DB transaction.
- **Instance upsert**: keyed on natural key `instance_id`.

---

## 4. Console-side data model (R1, R2)

### 4.1 New table `creaves_instances`

| Column | Type | Notes |
|--------|------|-------|
| `id` | char(36) PK | console-generated UUID |
| `instance_id` | varchar(255) UNIQUE NOT NULL | the wire `instance_id` (natural key) |
| `name` | varchar(255) NULL | display name from `instance` block; fallback = `instance_id` |
| `description` | varchar(255) NULL | |
| `first_seen_at` | datetime NOT NULL | first event/instance-block ever received |
| `last_seen_at` | datetime NOT NULL | last webhook call carrying this instance |
| `last_event_at` | datetime NULL | `MAX(event_streams.created_at)` for the instance (maintained on ingest) |
| `created_at` / `updated_at` | datetime | |

Registration rules:

- **Lazy auto-registration**: the first time the webhook handler sees an unknown
  `instance_id` (from an event or the `instance` block), the row is created inside the
  same transaction. No manual "add instance" step exists.
- `name`/`description` update only when the `instance` block carries non-empty values.
- The console **never** rejects events from unregistered instances (v1 behavior preserved).

### 4.2 `consolidated_animals` additions

| Column | Type | Notes |
|--------|------|-------|
| `translations` | json NULL | latest `translations` object (§3.4); cleared on state event if absent there |
| `state_hash` | char(64) NULL | `content_hash` from the last applied `animal_state` |
| `last_state_at` | datetime NULL | `created_at` of the last applied `animal_state` |

`event_count` semantics (clarified): counts **lifecycle events applied**
(discovered/status_changed/released/died). `animal_state` events **do not** increment it —
they replace state. This keeps `event_count` meaningful after any number of resyncs.

### 4.3 Reporting scoping (R2)

Every read path that aggregates data — dashboard (`/`, `/dashboard`), consolidated
animal list, and all report pages (`by_location`, `by_type`, `by_species`, reports index) —
accepts an optional `instance_id` query parameter:

- Absent/empty ⇒ **global view** (all instances) — current behavior.
- Present ⇒ scoped to that instance; unknown instance_id renders 404 with a clear message
  (no silently empty pages).
- UI: an **instance selector** (dropdown of registered instances + "All instances") in
  the navbar of dashboard/reports/list pages; selection propagates via query string to
  all report links on the page (partial/helper renders links with the current scope).
- The dashboard additionally shows **per-instance summary cards** (animal counts by
  status, last seen) when in global view.

### 4.4 Instance administration & cleanup (R1)

New admin pages under `/instances` (admin session role required):

- `GET /instances` — list: name, instance_id, first/last seen, last event, animal count,
  event count, linked API keys.
- `GET /instances/{instance_id}` — detail + **cleanup** panel.
- `POST /instances/{instance_id}/cleanup` — **single action**, admin + CSRF:

  In one DB transaction, delete everything belonging to the instance:

  1. `event_streams` rows (`instance_id = X`)
  2. `consolidated_animals` rows (`instance_id = X`)
  3. `webhook_api_keys` rows **restricted to** `instance_id = X` (they are orphaned by design)
  4. `creaves_instances` row for `X`

  UI: confirm dialog showing exact counts of what will be deleted
  (animals / events / keys) and requiring the admin to type the `instance_id` to confirm.
  After cleanup, a flash message documents the recovery path: *trigger a full resync on
  the creaves instance* (§6) — the console re-registers lazily on first event.
  Non-admin gets 403; unknown instance 404. Idempotent: repeated call → 404 (already gone).

---

## 5. Console multilingual UI (R5, R6)

- Add **French**: `locales/all.fr.yaml` + `.plush.fr.html` variants for every template
  (pattern identical to existing de/nl variants); `uiLanguages` gains
  `{"fr", "Français"}` in `actions/render.go`; `normalizeUILang` passes `fr` through
  (already does).
- Reference values shown on list/detail/report pages render via a new template helper
  `tfield_localized(animal_or_label, field)`:
  resolve `animal.translations[current_lang][field]`, fallback canonical value.
- Report grouping keys stay canonical; group labels resolve through the same helper using
  the translation map of any representative row (first row of the group) — implemented as
  a per-request canonical→localized lookup built from `consolidated_animals.translations`.

---

## 6. Creaves-side: full resync (R3)

### 6.1 Scope of "all content"

One `animal_state` event per animal (existing rows in `animals`, ordered by `id`),
covering the complete current state of the contract entities: animal + discovery +
intake + outtake (if any) + current status + translations. This equals what the console
can store today; extending the contract (cares, treatments, …) is a future spec amendment.

### 6.2 Producer data model

New migration on creaves:

- `event_streams.content_hash` char(64) NULL (+ index `(event_type, content_hash)`) —
  set for `animal_state` events.
- `event_streams.resync_run_id` char(36) NULL (+ index) — links events to their run for
    progress queries.
- New table `resync_runs`:

| Column | Type | Notes |
|--------|------|-------|
| `id` | char(36) PK | |
| `status` | varchar(20) NOT NULL | `running` / `completed` / `failed` / `cancelled` |
| `total_animals` | int NOT NULL | snapshot count at start |
| `animals_processed` | int NOT NULL | animals examined (created-or-skipped) |
| `events_created` | int NOT NULL | |
| `events_skipped_unchanged` | int NOT NULL | |
| `events_delivered` | int NOT NULL | derived live: run events with `delivered_at IS NOT NULL` |
| `errors` | text NULL | JSON array of `{animal_id, error}` |
| `started_at` / `finished_at` | datetime / NULL | |

Guard: single active run per instance — a `running` row blocks new starts (409 + flash).

### 6.3 Resync flow

1. Admin opens `GET /webhook_resync` (admin only; page available only when webhook
   enabled+configured, otherwise shows setup guidance).
2. `POST /webhook_resync` — validates webhook config, creates `resync_runs` row
   (`status=running`, `total_animals` = count), starts a **background goroutine**
   (does not block the request), redirects to the progress page.
3. Worker loop (per animal, id order):
   - build state payload (`animal` + `discovery` + `intake` + `outtake` + statuses +
     `translations` for all locales) via the shared payload builder;
   - compute `content_hash` + deterministic UUID (§3.5);
   - if an existing `animal_state` row for this animal has the same `content_hash`:
     increment `events_skipped_unchanged` (and if that row is undelivered, leave it queued);
   - else create `event_streams` row (`event_type=animal_state`, `content_hash`,
     `resync_run_id`, `delivered_at=NULL`);
   - update `animals_processed` (+ errors on failure);
   - cooperative cancel check each iteration.
4. Delivery reuses the **existing WebhookPusher** queue/worker untouched (same batching,
   rate limit, circuit breaker, retry). `events_delivered` is a live count over the run's
   events. Rate limiting is therefore identical to normal operation; the progress page
   shows an ETA computed from current throughput.
5. Worker end: set `status=completed|failed|cancelled`, `finished_at`. App restart with a
   `running` row ⇒ on boot, mark it `failed` with error `"interrupted by restart"` (the
   admin re-runs; content-addressed IDs make that safe).
6. Cancel (`POST /webhook_resync/cancel`): marks `cancelled`, stops enqueueing.
   Already-created events stay in the normal delivery queue (they are valid state events).

### 6.4 Progress web view (R3)

`GET /webhook_resync` shows:

- no run: start button (+ explanation + warning that a resync is safe/idempotent).
- active run: progress bar (`animals_processed / total_animals` for snapshot phase;
  `events_delivered / events_created` for delivery phase), counts (created / skipped
  unchanged / delivered / errors), throughput + ETA, cancel button, last 10 errors.
- past runs: table of recent runs with status/counts/timestamps.

Real-time: the page polls `GET /webhook_resync/status.json` (admin session, no CSRF
needed — GET) every **2 s** and re-renders the widget client-side (vanilla JS, no new
frontend dependencies; console has no webpack pipeline — plain template + fetch).

---

## 7. Compatibility matrix

| Producer → Receiver | Behavior |
|---------------------|----------|
| v2 creaves → v2 console | full spec |
| v1 creaves → v2 console | v1 path exactly as today; instance auto-registered lazily; no translations stored (UI falls back to canonical) |
| v2 creaves → v1 console | unknown envelope fields ignored by Go unmarshal; `animal_state` events are treated as generic events — v1 console merges fields (no replace semantics, no dedup by content) — acceptable during rollout window; upgrade consoles first |
| v2 resync → v1 console | works (merge semantics), slightly inflated `event_count` on old console only |

Rollout order is therefore: **console first, then creaves**.

---

## 8. Security & safety

- Cleanup, resync start/cancel: admin role + CSRF (session forms). `status.json`:
  admin session, GET.
- All new SQL uses parameter binding. (Fixes existing `fmt.Sprintf` interpolation of
  `year` in `ReportsBySpecies` — see plan finding F1.)
- Cleanup transaction is all-or-nothing; no cascading deletes via FK (tables are not FK-linked).
- Bearer key auth unchanged; instance-restricted keys still enforced per event.

---

## 9. Acceptance criteria (per requirement)

- **R1**: an admin can delete an instance (data + registry + its restricted keys) from
  `/instances/{id}` in one confirmed action; DB shows zero residual rows for that
  `instance_id` across `event_streams`, `consolidated_animals`, `creaves_instances`,
  `webhook_api_keys`; repeated call → 404.
- **R2**: every dashboard/report/list page renders identical, correct aggregates with no
  `instance_id` param (global) and scoped aggregates with a valid one; selector navigates
  and preserves scope across report links.
- **R3**: starting a resync returns immediately; progress page shows live snapshot+delivery
  progress that converges to totals without reload; cancel stops enqueueing; a second
  identical resync creates **0 new events** (all skipped-unchanged / console dedup).
- **R4**: replaying any webhook batch, resync run, or cleanup produces no duplicate data
  (verified by row counts and content assertions in tests).
- **R5**: every event payload carries `translations` with all producer locales that have
  values for the reference fields; console list/detail/report pages display localized
  labels for fr/en-US/de/nl with canonical fallback.
- **R6**: console UI fully navigable in French (all templates have fr variants; language
  switcher offers Français).
