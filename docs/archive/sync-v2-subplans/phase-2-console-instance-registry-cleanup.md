# Phase 2 — Console instance registry, state events & cleanup

Parent: [`../SYNC_IMPLEMENTATION_PLAN.md`](../SYNC_IMPLEMENTATION_PLAN.md) · Spec: [`../../specs/SYNC_SPEC.md`](../../specs/SYNC_SPEC.md) (§3.2–§3.5, §4.1, §4.2, §4.4)
Findings fixed: **F3** (no instance entity), **F6** (no resync semantics). Requirements: **R1**, **R2 base**, **R4 base**.
Internal order: T2.1 → T2.2 → T2.3 → T2.4 → T2.5 (sequential).

## T2.1 `CreavesInstance` model + migration

- Files: new `creaves-console/models/creaves_instance.go`; `migrations/2026MMDDHHMMSS_create_creaves_instances.up.fizz` + `.down.fizz`; update SQLite DDL in test `TestMain`s; new `models/creaves_instance_test.go`.
- **RED** `TestCreavesInstance_ValidateRequiresInstanceID`: empty `instance_id` → validation error.
- **RED** `TestCreavesInstance_UpsertCreatesThenUpdates`: upsert new (`instance_id=center-north`, name nil) → row exists, `name` NULL, `first_seen_at` set; upsert again with name "Center North" → same row (count 1), name updated, `first_seen_at` unchanged, `last_seen_at` >= previous.
- **GREEN**: model with `InstanceID` unique; `UpsertByInstanceID(tx, id, name, description, now)` helper implementing natural-key upsert.

## T2.2 Webhook auto-registration + instance block (spec §3.2, §4.1)

- Files: `actions/webhook.go`; tests appended to `actions/webhook_test.go` (sqlite).
- **RED** `TestWebhookEventsHandler_AutoRegistersUnknownInstance`: post a single v1 event for new instance → `creaves_instances` has exactly 1 row with that `instance_id`, `first_seen_at == last_seen_at`, `last_event_at == event.CreatedAt`.
- **RED** `TestWebhookEventsHandler_InstanceBlockUpserts`: envelope contains `"instance":{"id":"center-north","name":"Center North"}` + event → row created with name; second call with different name → updated, count still 1.
- **RED** `TestWebhookEventsHandler_InstanceBlockMismatchFailsEvent`: `instance.id != event.instance_id` → response `errors` non-empty, `processed == 0`, no rows written for the event.
- **RED** `TestWebhookEventsHandler_V1SenderStillWorks`: no `contract_version`, no `instance` block → 200 `{"processed":1}` (regression guard).
- **GREEN**: parse optional `ContractVersion int` + `Instance *InstanceInfo` on the envelope; upsert before the event loop; per-event mismatch check mirroring the restricted-key check at `webhook.go:93-96`; update `last_event_at` when `event.CreatedAt` is newer.

## T2.3 `consolidated_animals` v2 columns (spec §4.2)

- Files: fizz migration adding `translations json null`, `state_hash char(64) null`, `last_state_at datetime null`; SQLite DDL in tests.
- **RED** `TestConsolidatedAnimal_HasStateColumns`: create animal with the three fields set via raw update → read back, values round-trip (`translations` JSON string equal after normalize).
- GREEN: columns only (no behavior yet).

## T2.4 `animal_state` processing — replace semantics (spec §3.3, §3.5, §4.2)

- Files: `models/event_stream.go` (add `EventTypeAnimalState` + `Translations map[string]map[string]string` to `EventPayload`), `models/consolidated_animal.go`, `actions/event_processor.go`; tests in `models/consolidated_animal_test.go`.
- **RED** `TestApplyEvent_AnimalStateReplacesFields`: consolidated row has `species=Hérisson, cage=A12, zone=Quarantine` from earlier events; apply `animal_state` with `species=Hérisson, cage=""(absent), zone=Zone 2` → result `zone=Zone 2`, `cage` **NULL** (cleared, not stale), `current_status` from payload.
- **RED** `TestApplyEvent_AnimalStateDoesNotIncrementEventCount`: prior `event_count=3`; apply state event → still 3; `state_hash` and `last_state_at` set.
- **RED** `TestApplyEvent_AnimalStateStoresTranslations`: payload translations `{"en-US":{"species":"Hedgehog"}}` → `translations` column JSON equal; a later state event without translations clears it to NULL.
- **RED** `TestProcessEvent_AnimalStateUnknownEventTypeIsNoop` (regression guard): event with `event_type=animal_state` on v1-shaped `UpdateFromPayload` path doesn't panic; unknown types leave status untouched.
- **GREEN**: `UpdateFromPayload` gains `applyState(payload)` branch used only for `animal_state`: full overwrite of the mutable field set, `EventCount` untouched; processor stores the producer-supplied `state_hash` payload key (if present) into `state_hash` — recompute nothing.

## T2.5 Instance admin UI + cleanup (R1; spec §4.4)

- Files: new `actions/instances.go` (list/show/cleanup), templates `templates/instances/{index,show}.plush.{html,de.html,nl.html,fr.html}` (fr copies initially from en-US until Phase 4), routes in `app.go` (`GET /instances`, `GET /instances/{instance_id}`, `POST /instances/{instance_id}/cleanup`, admin-only group), tests `actions/instances_test.go` (sqlite).
- **RED** `TestInstancesIndex_ListsRegisteredInstances`: seed 2 instances + counts → page contains both names and animal counts.
- **RED** `TestInstanceCleanup_DeletesEverything`: seed instance X: 3 `event_streams`, 2 `consolidated_animals`, 1 registry row, 1 API key restricted to X, 1 global key, 1 key restricted to other instance Y; `POST /instances/X/cleanup` (admin session) → 0 rows for X in all three data tables, X's key gone, global + Y keys intact, instance Y data intact; response redirects with success flash.
- **RED** `TestInstanceCleanup_RequiresAdmin`: non-admin session → 403.
- **RED** `TestInstanceCleanup_UnknownInstance404`.
- **RED** `TestInstanceCleanup_Idempotent`: second call → 404, no error page.
- **RED** `TestInstanceCleanup_RequiresTypedConfirmation`: missing/mismatched typed `instance_id` confirmation → 422, rows intact.
- **RED** `TestPurgeInstance_Transactional` (unit level): `purgeInstance(tx, id)` run on a connection where `webhook_api_keys` table was dropped → returns error; in a separately seeded DB verify that when any step errors, earlier steps' deletes are not committed.
- **GREEN**: `purgeInstance(tx, instanceID)` executes the 4 deletes in order inside one transaction (event_streams → consolidated_animals → instance-restricted webhook_api_keys → creaves_instances); controller calls it; confirm form requires typed `instance_id` match, validated server-side; success flash documents recovery path (trigger full resync on the creaves side).
