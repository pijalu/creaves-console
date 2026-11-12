# Phase 5 — Creaves payload v2 (translations + instance block)

Parent: [`../SYNC_IMPLEMENTATION_PLAN.md`](../SYNC_IMPLEMENTATION_PLAN.md) · Spec: [`../../specs/SYNC_SPEC.md`](../../specs/SYNC_SPEC.md) (§3.2, §3.4)
Findings fixed: **G2** (canonical-only payloads), **G5** (no instance metadata). Requirement: **R5** producer side.
Depends on: Phase 2 shipped (console accepts v2 fields without breaking).

## T5.1 Translations map in payload builder

- Files: `creaves/models/event_stream.go` (`Translations map[string]map[string]string` on `EventPayload`), `creaves/actions/event_producer.go` (+ new `actions/event_translations.go` loader), tests `creaves/actions/event_producer_test.go`.
- **RED** `TestBuildEventPayload_IncludesAllLocales`: animal with species "Hérisson"; translations table rows for species (en-US "Hedgehog", de "Igel", nl "Egel") and animal type (en-US "Mammal", de/nl missing) → payload.Translations has keys `fr,en-US,de,nl`; `fr.species == "Hérisson"`; `de.species=="Igel"`; `de` lacks `animal_type` key (omitted, not empty string).
- **RED** `TestBuildEventPayload_NoTranslationsStillCanonical`: no translation rows → `Translations` nil/empty (console falls back) — payload still valid.
- **RED** `TestBuildEventPayload_OuttakeAndZoneAndEntryCauseTranslated`: same assertions for `outtake_type`, `zone`, `entry_cause` (entry_cause value = the `Fmt(false)` string used in the payload; translation resolved by `record_id` on `entry_causes` field `cause`).
- **GREEN**: one bulk query per (table, field) across the 4 locales reusing the `translations` table (`tname.go` access patterns, `models.SupportedLocales`); map assembled per spec §3.4 closed field set (`species`, `animal_type`, `animal_age`, `zone`, `outtake_type`, `entry_cause`); empty values omitted; `fr` filled from canonical base.

## T5.2 Instance block + contract version in pusher envelope (spec §3.2)

- Files: `creaves/actions/webhook_pusher.go` (envelope struct), tests `creaves/actions/webhook_pusher_test.go` (httptest receiver capturing body).
- **RED** `TestDeliverBatch_EnvelopeHasInstanceAndVersion`: fake console server asserts JSON body has `contract_version==2` and `instance.id == config.InstanceID`, `instance.name == config.Name`.
- **GREEN**: extend `deliverBatch` envelope struct (`dashboard.go` counterpart on console already parses it — Phase 2 T2.2); config `Name` already on `models.Config` (`configs.go:44-48`).
