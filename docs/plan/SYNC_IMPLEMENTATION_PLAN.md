# Sync v2 — Review & Implementation Plan (TDD)

Companion to [`../specs/SYNC_SPEC.md`](../specs/SYNC_SPEC.md). This document contains:

1. **Review findings** of the current creaves ↔ console sync (evidence: file:line).
2. **Subplan index** — micro-tasks live in [`subplans/`](./subplans/), one file per phase.
3. **Requirement traceability** and **sequencing/risks**.

Implementation detail (micro-tasks + TDD test cases) is intentionally NOT in this file —
each phase has its own subplan:

| # | Subplan | Scope |
|---|---------|-------|
| 1 | [`subplans/phase-1-console-hardening.md`](./subplans/phase-1-console-hardening.md) | SQL injection fix (F1) |
| 2 | [`subplans/phase-2-console-instance-registry-cleanup.md`](./subplans/phase-2-console-instance-registry-cleanup.md) | Instance registry, webhook auto-registration, `animal_state` replace semantics, instance admin UI + cleanup (R1, R2 base; F3, F6) |
| 3 | [`subplans/phase-3-console-reporting-scope.md`](./subplans/phase-3-console-reporting-scope.md) | Global / per-instance scoping of dashboard + all reports (R2; F2) |
| 4 | [`subplans/phase-4-console-multilingual.md`](./subplans/phase-4-console-multilingual.md) | Console French locale + localized reference-data display (R5/R6; F4, F5) |
| 5 | [`subplans/phase-5-creaves-payload-v2.md`](./subplans/phase-5-creaves-payload-v2.md) | Translations map in payloads, instance block + contract version (R5; G2, G5) |
| 6 | [`subplans/phase-6-creaves-full-resync.md`](./subplans/phase-6-creaves-full-resync.md) | Content-addressed state events, background resync service, progress UI (R3, R4; G1) |
| 7 | [`subplans/phase-7-e2e-docs.md`](./subplans/phase-7-e2e-docs.md) | Cross-project e2e tests, manual checklist, docs updates |

## Conventions (apply to every subplan)

- Console tests: `cd creaves-console && CGO_ENABLED=1 go test -tags sqlite ./actions/... ./models/...`
  (single test: add `-run TestName`). New DB-touching tests go in files with
  `//go:build sqlite` and reuse the `TestMain` harness in `actions/event_processor_test.go`
  / `models/models_test.go` (add new tables to the SQLite DDL there).
- Creaves tests: `cd creaves && go test ./actions/... ./models/...` (MySQL test DB required).
- Every task follows: **RED** (write failing test, run, observe failure) → **GREEN**
  (minimal implementation) → **REFACTOR if needed** → full package suite green.
- Each phase ends with the full suite for both projects (where that project changed).

---

## Part A — Review findings (current state)

### Console (`creaves-console`)

| ID | Severity | Finding | Evidence |
|----|----------|---------|----------|
| F1 | **High (security)** | SQL injection: `year` query param interpolated into SQL via `fmt.Sprintf` | `actions/dashboard.go:407-430` (`ReportsBySpecies`) |
| F2 | High (R2 gap) | Reports & dashboard have **no instance scoping**; only `ConsolidatedAnimalsIndex` supports `view_mode=instance` | `actions/dashboard.go:14-95` (DashboardIndex), `:237-304` (ReportsIndex), `:307-364`, `:367-398`, `:402-446`; scoping exists only at `:108-116` |
| F3 | High (R1 gap) | No instance entity: `instance_id` is a bare string; no registry, no admin UI, no cleanup/purge path anywhere | `migrations/schema.sql` (no such table); no `/instances` routes in `actions/app.go:41-77` |
| F4 | Medium (R5 gap) | Payloads carry only canonical French reference values; console stores no translations; UI cannot display localized `species`/`animal_type`/`animal_age`/zone/outtake type/entry cause | `models/event_stream.go:39-125` (no translations field); both `AGENTS.md` "Canonical values" note ("future translations map … out of scope") |
| F5 | Medium (R6 gap) | Console UI languages: en-US, de, nl — **fr missing** although producers are French-first | `actions/render.go:19-26`; `locales/` = `all.de.yaml, all.en-us.yaml, all.nl.yaml`; no `*.plush.fr.html` in `templates/` |
| F6 | Medium (R4 gap) | No resync semantics: replaying identical state under new UUIDs re-applies events, inflating `event_count` and re-writing identical data; `UpdateFromPayload` is merge-only (cannot intentionally replace/clear fields, e.g. a corrected empty cage stays stale) | `models/consolidated_animal.go:74-178` (merge + `EventCount++` at `:177`); no `animal_state` in `models/event_stream.go:18-23` |
| F7 | Low (perf) | API-key auth loads **all** active keys and bcrypt-compares each on every webhook call | `actions/webhook.go:173-186` |
| F8 | Low | `source_db` column still present (deprecated) | `migrations/schema.sql:79`, `actions/webhook.go:138` |
| F9 | Low | `ConsolidationRunner.RunDryRun` not implemented | `actions/consolidation_runner.go:88-92` |

Console strengths to preserve: UUID dedup incl. self-healing redelivery of
unprocessed duplicates (`webhook.go:105-129`); per-event `processed_ids` so the creaves
pusher only marks accepted events delivered (`webhook.go:158-167` ↔
`creaves/actions/webhook_pusher.go:285-322`); solid sqlite e2e harness reconstructing the
pusher wire format (`actions/webhook_e2e_test.go`).

### Creaves (`creaves`)

| ID | Severity | Finding | Evidence |
|----|----------|---------|----------|
| G1 | High (R3 gap) | No "full resync" for operators: only CLI grifts `event:snapshot` (skips animals that already have events) and `event:snapshot:force` (creates duplicate events with fresh UUIDs — **not idempotent**, inflates console `event_count`); no background execution, no progress UI | `grifts/event_snapshot.go:17-117` (skip logic), `:121-209` (force), `:213-224` (clean = "not yet implemented") |
| G2 | Medium (R5 gap) | Payload builder emits canonical values only, no translations map | `actions/event_producer.go:65-211` |
| G3 | Medium | Snapshot events are ordered per-animal (discovered then released/died) with `created_at=now`, so console replays history out of real chronological order; a state-based resync (spec §6) supersedes this concern | `grifts/event_snapshot.go:66-105` |
| G4 | Low (done) | Worker-not-started-at-boot WIP from AGENTS.md is **already fixed**: `InitWebhookAtBoot()` exists and is called from `cmd/app/main.go:24` | `actions/webhook_pusher.go:367-379` |
| G5 | Low | Envelope carries no instance metadata / contract version | `actions/webhook_pusher.go:228-252` |

Test-idempotency note (console side, existing): `event_processor_test.go` already covers
UUID dedup and multi-instance isolation — new tasks extend rather than replace it.

---

## Part B — Phase & dependency overview

Phases are ordered for safe rollout (**console first** — spec §7). Dependencies:

```
Phase 1 (F1 fix)          — independent, land first
Phase 2 (instances+state) — T2.1→T2.2→T2.3→T2.4→T2.5 sequential inside phase
Phase 3 (report scope)    — needs T2.1 (registry) for dropdown/404; else independent
Phase 4 (console i18n)    — independent of 2–3 (templates touch all phases at the end)
Phase 5 (creaves payload) — needs Phase 2 shipped (console accepts v2 fields)
Phase 6 (creaves resync)  — needs Phase 5 (translations in payloads) + T2.4 (state events)
Phase 7 (e2e + docs)      — last; covers everything
```

Full micro-tasks per phase: see subplan index above.

---

## Part C — Requirement → task traceability

| Req | Tasks |
|-----|-------|
| R1 cleanup | Phase 2: T2.1, T2.2, T2.5; Phase 7: cleanup+resync recovery e2e |
| R2 instances + scoped reporting | Phase 2: T2.1, T2.2; Phase 3: T3.1, T3.2; Phase 7 e2e |
| R3 full resync + progress UI | Phase 6: T6.1–T6.4; Phase 7: e2e + manual checklist |
| R4 idempotency | Phase 2: T2.4 (replace semantics); Phase 6: T6.2, T6.3 (skip-unchanged, single-run); Phase 7: replay e2e |
| R5 all languages in payloads | Phase 5: T5.1; Phase 4: T4.2 + Phase 2: T2.4 (storage); Phase 7 e2e |
| R6 console fr locale | Phase 4: T4.1 |
| Security fix (F1) | Phase 1: T1.1 |

## Part D — Risks & sequencing notes

- **Console-first rollout** is mandatory (spec §7): `animal_state` on a v1 console degrades
  to merge semantics.
- `event_count` semantics change is display-only; verify no template divides by it.
- Creaves resync rate = existing `WebhookMaxPerMin` (default 60/min). For large backfills,
  admins raise the limit temporarily — surfaced in the resync UI hint (no separate limiter in v2).
- SQLite (tests) vs MySQL (dev/prod): new migrations must be valid in both; JSON columns
  exist in both (SQLite stores TEXT — existing `payload` column already does this).
- `translations` payload size: ≤ 4 locales × 6 fields ≈ small; no compression needed.
