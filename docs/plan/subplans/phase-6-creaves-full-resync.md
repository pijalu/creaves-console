# Phase 6 — Creaves full resync (background + progress UI)

Parent: [`../SYNC_IMPLEMENTATION_PLAN.md`](../SYNC_IMPLEMENTATION_PLAN.md) · Spec: [`../../specs/SYNC_SPEC.md`](../../specs/SYNC_SPEC.md) (§3.3, §3.5, §6)
Findings fixed: **G1** (no idempotent full resync). Requirements: **R3**, **R4**.
Depends on: Phase 5 (translations in payloads) + Phase 2 T2.4 (console `animal_state` handling).
Internal order: T6.1 → T6.2 → T6.3 → T6.4.

## T6.1 `resync_runs` model + `event_streams` columns

- Files: `creaves/models/resync_run.go`, fizz migrations (table + `content_hash`, `resync_run_id` columns + indexes `(event_type, content_hash)`), test DDL/fixtures.
- **RED** `TestResyncRun_Lifecycle`: create running run → mark completed with counts → status/finished_at set; `HasActiveRun` true while running, false after.
- **GREEN**: model + status helpers.

## T6.2 Deterministic state event identity (spec §3.5)

- Files: `creaves/actions/event_producer.go` (or new `resync_hash.go`); tests `creaves/actions/resync_hash_test.go` (pure).
- **RED** `TestStateContentHash_DeterministicAcrossRuns`: same animal data + translations → identical 64-hex hash on two builds.
- **RED** `TestStateContentHash_VolatileFieldsExcluded`: differing `timestamp`/`user_id` → same hash; differing `cage` → different hash.
- **RED** `TestStateEventUUID_StableFormula`: UUID == `uuid.NewV5(NAMESPACE_CREAVES_STATE, instanceID+"|"+animalID+"|"+hex(hash))` with the spec literal namespace — assert exact expected UUID for a fixed fixture (golden vector computed once, hard-coded in test).
- **GREEN**: canonical builder exactly per spec §3.5 field order + `\x1f` join + sorted-key translations JSON.

## T6.3 Resync service (background worker; spec §6.3)

- Files: new `creaves/actions/webhook_resync.go`; tests `creaves/actions/webhook_resync_test.go`.
- **RED** `TestStartResync_RejectsWhenWebhookDisabled`: disabled config → error, no run row.
- **RED** `TestStartResync_SingleActiveRun`: two consecutive starts → second returns conflict error; exactly 1 `running` row.
- **RED** `TestResync_CreatesStateEventsWithDeterministicIDs`: run over 3 animals (1 with outtake) → 3 `event_streams` rows `event_type=animal_state`, `content_hash` set, `resync_run_id` set, UUIDs == formula output; `total_animals=3`, `animals_processed=3`, `events_created=3`.
- **RED** `TestResync_SkipsUnchangedAnimals`: run twice; second run creates 0 new events, `events_skipped_unchanged=3` (existing delivered state rows found by `(animal_id, content_hash)`).
- **RED** `TestResync_UndeliveredSameHashNotDuplicated`: pre-mark existing same-hash row `delivered_at=NULL` → no duplicate created, run counts it as skipped, row still undelivered (stays queued).
- **RED** `TestResync_CancelStopsEnqueueing`: start, cancel after first animal → status `cancelled`, `animals_processed < total`, created events remain queued.
- **RED** `TestResync_IndividualAnimalErrorRecorded`: one animal fails payload build → run continues, `errors` JSON contains `{animal_id, error}`, final status `completed` (per-animal errors don't fail the run; `failed` reserved for run-level aborts).
- **RED** `TestResync_BootMarksInterruptedRunFailed`: seed `running` row; call `RecoverInterruptedRuns(DB)` → status `failed`, error `"interrupted by restart"` (wired into the `InitWebhookAtBoot` path / `cmd/app/main.go`).
- **GREEN**: goroutine loop per spec §6.3; progress fields updated in DB as it advances (status.json survives restarts); reuse `EnsureWebhookWorkerRunning()` so delivery starts immediately; delivery itself reuses the existing WebhookPusher queue untouched (batching, rate limit, circuit breaker, retry).

## T6.4 Resync web UI + status endpoint (spec §6.4)

- Files: `creaves/actions/webhook_resync_handlers.go`, `templates/webhook_resync/{index,status}.plush.html` (+de/nl/fr variants — creaves 4-locale pattern), routes (admin group), plain-JS 2 s polling snippet in template.
- **RED** `TestWebhookResync_IndexRequiresAdmin` (403 non-admin).
- **RED** `TestWebhookResync_StartRedirectsAndCreatesRun`: POST start → 303 to index, run row exists, response returned immediately (no blocking on the worker).
- **RED** `TestWebhookResync_StatusJSON`: GET `status.json` while run active → JSON `{status,total_animals,animals_processed,events_created,events_skipped_unchanged,events_delivered,throughput,eta_seconds,recent_errors[]}`; delivered count == `SELECT COUNT(*) WHERE resync_run_id=? AND delivered_at IS NOT NULL`.
- **RED** `TestWebhookResync_StatusJSONNoRun`: `{status:"none"}`.
- **RED** `TestWebhookResync_Cancel`: POST cancel → run cancelled.
- **GREEN**: handlers thin over T6.3 service; nav entry under admin menu (event streams section), i18n keys in new `locales/webhook_resync.*.yaml`; page states per spec §6.4 (no run → start button; active → progress bar + ETA + cancel; past runs table).
