# Phase 7 — End-to-end verification & documentation

Parent: [`../SYNC_IMPLEMENTATION_PLAN.md`](../SYNC_IMPLEMENTATION_PLAN.md) · Spec: [`../../specs/SYNC_SPEC.md`](../../specs/SYNC_SPEC.md)
Requirements verified: **all (R1–R6)**. Depends on: Phases 1–6 complete.

## T7.1 Console e2e: v2 flow (extends `actions/webhook_e2e_test.go`)

- **RED** `TestE2E_V2Resync_IdempotentAndLocalized`: build state events for 2 animals via the *documented wire shape* (translations included, deterministic UUIDs recomputed in test per spec §3.5 formula) → deliver → assert consolidated rows + translations stored; deliver the **identical batch twice** → row counts unchanged, `event_count` unchanged, response second time `processed==2` via dedup; change one animal's zone (new UUID) → zone replaced, `cage` cleared when state omits it.
- **RED** `TestE2E_V2CleanupThenResync`: purge instance (Phase 2 helper) → re-deliver same state events → instance auto-registered again, animals rebuilt with identical final state (proves R1+R3 recovery path).
- **RED** `TestE2E_InstanceScopedReports`: two instances → scoped report numbers per Phase 3 assertions at HTTP level.

## T7.2 Cross-project manual test checklist (documented, not automated)

- Both `buffalo dev` instances; config wired (creaves 3000 → console 3001).
- Create/edit animals → dashboard reflects in <10 s.
- Console: switch language fr/en-US/de/nl on list + by_species report → localized labels.
- Creaves: run full resync from UI → progress advances without reload; second run all "skipped unchanged".
- Console: cleanup instance → creaves: resync again → data back, identical.

## T7.3 Documentation updates (no code)

## Validation record

- Console and Creaves full Go suites pass with SQLite/CGO and standard test commands.
- v2 state-event e2e covers localized storage, replacement semantics, and UUID replay deduplication.
- Manual checklist remains deployment-specific: both Buffalo apps, webhook configuration, language switching, resync progress, cleanup/recovery.

## T7.2 execution record

Run date: 2026-08-22 (local development workspace).

| Check | Evidence | Result |
|---|---|---|
| Console build | `go build ./...` in `creaves-console/` | PASS |
| Creaves build | `go build ./...` in `creaves/` | PASS |
| Console automated regression | `CGO_ENABLED=1 go test -tags sqlite ./...` | PASS |
| Creaves automated regression | `go test ./...` (local MySQL test service) | PASS |
| Database prerequisite | `mysqladmin ping -h 127.0.0.1 -P 3306 -u creaves -pcreaves` | PASS (`mysqld is alive`) |
| Console dev service | `buffalo dev` using `.buffalo.dev.yml` (`PORT=3001`), `curl http://127.0.0.1:3001/` | PASS (HTTP 302) |
| Cross-project operator flow | Create/edit animal, language switching, UI resync, cleanup/recovery | NOT RUN: requires configured operator session, API key, and live Creaves data; execute rows below in deployment environment |

### Deployment operator sign-off

Execution environment: local macOS development workspace, 2026-08-22, operator: autonomous deployment check. Evidence commands below use `127.0.0.1`; raw API keys are intentionally omitted.

| Row | Evidence | Result |
|---|---|---|
| Start both services and verify roots | `lsof -nP -iTCP:3000 -sTCP:LISTEN` and port 3001 showed `tmp/creaves-build` and `tmp/creaves-console-build`; `curl -D - http://127.0.0.1:3000/` and `:3001/` each returned `HTTP/1.1 302 Found` to `/auth/new` | **PASS** |
| Configure webhook URL/key/instance | Console admin login (`admin`/`admin123`) succeeded; `buffalo task db:seed` reported `Admin user already exists`; API JSON create returned `201` and key prefix; Creaves UI PUT was attempted with URL/key/instance but returned `500` (`config/edit.plush.html: line 6: ... "settings": unknown identifier`) and did not persist. Disposable configuration was then applied through local MySQL for transport-only validation; stored settings query confirmed URL, enabled flag, instance `t72-local` | **FAIL (UI)**; transport setup **PASS** |
| Create/edit animal and dashboard update under 10 seconds | Authenticated webhook POST after Console migrations returned `200` with `{"processed":1,...}`; MySQL queries confirmed `event_streams` row (`t72-local`, animal 7202, processed timestamp) and `consolidated_animals` row (`T7.2 Disposable Fox`). No Creaves animal-form create/edit was completed; dashboard HTML search did not observe row | **FAIL (UI sign-off)**; receiver transport **PASS** |
| Switch `fr`, `en-US`, `de`, `nl`; list and by_species labels | Four authenticated `/lang/?lang=...&url=/dashboard` requests each returned `302`; no rendered list/by_species label assertions or screenshots captured | **FAIL (insufficient UI evidence)** |
| Full resync progress without reload; rerun all skipped unchanged | Creaves `/webhook_resync` route was reachable from authenticated navigation, but no full resync run or progress observation was completed | **FAIL (not exercised)** |
| Console cleanup then Creaves resync restores identical state | No cleanup/resync run completed; no before/after row comparison captured | **FAIL (not exercised)** |
| Operator/environment/time/screenshots or logs | Environment/operator/time recorded above; command output captured in execution session, but no screenshots | **FAIL (screenshots absent)** |

Root cause/blocker for UI configuration row: Creaves update handler renders `config/edit.plush.html` on its error path without setting template variable `settings`; request failed before persistence. This deployment record intentionally preserves observed failures rather than treating SQL setup as UI sign-off.

- Update both `AGENTS.md` webhook-contract sections → point to `creaves-console/docs/specs/SYNC_SPEC.md`; add `/instances`, `/webhook_resync` to key files / routes tables.
- Update `creaves-console/TODO.md` and `creaves/TODO.md` status lines for the sync v2 phases.