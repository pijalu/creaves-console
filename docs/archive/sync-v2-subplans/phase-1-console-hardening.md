# Phase 1 — Console hardening quick win

Parent: [`../SYNC_IMPLEMENTATION_PLAN.md`](../SYNC_IMPLEMENTATION_PLAN.md) · Spec: [`../../specs/SYNC_SPEC.md`](../../specs/SYNC_SPEC.md) (§8)
Findings fixed: **F1** (SQL injection, High)

## T1.1 Fix SQL injection in ReportsBySpecies (F1)

- Files: `creaves-console/actions/dashboard.go`, test `creaves-console/actions/dashboard_test.go` (new, sqlite tag).
- **RED** `TestReportsBySpecies_YearParamIsBound`:
  seed 2 animals (species A) year 2023 + 1 animal (species B) year 2024;
  request `GET /reports/by_species?year=2024 OR 1=1--` through the buffalo test app;
  assert response 200 **and** species A row absent / only species B present
  (with `?year=2024` baseline asserting B only; the injection payload must not widen results).
- **RED** `TestReportsBySpecies_YearInvalidIsIgnored`: `year=abc` renders 200 with all species (no SQL error).
- **GREEN**: replace `fmt.Sprintf` where-clause with `tx.RawQuery(query, year)` parameter binding
  (drop the `whereClause` string concatenation at `dashboard.go:408-410`/`421-430`).
- Acceptance: both tests green; manual grep — no remaining `Sprintf` building report SQL.
