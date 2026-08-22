# Phase 3 — Console reporting scoping (global / per-instance)

Parent: [`../SYNC_IMPLEMENTATION_PLAN.md`](../SYNC_IMPLEMENTATION_PLAN.md) · Spec: [`../../specs/SYNC_SPEC.md`](../../specs/SYNC_SPEC.md) (§4.3)
Findings fixed: **F2** (no instance scoping on reports/dashboard). Requirement: **R2**.
Depends on: Phase 2 T2.1 (registry table for dropdown + 404 handling).

## T3.1 Scope helper

- Files: new `actions/report_scope.go`; test `actions/report_scope_test.go` (pure, no sqlite tag).
- **RED** `TestResolveReportScope_GlobalAndInstance`: `""` → `ScopeAll{}`; `"center-north"` → `ScopeInstance{ID}`; helper returns SQL fragment + args, e.g. `(" instance_id = ? ", [id])` composable into every query.
- **GREEN**: tiny pure helper; templates get selected scope for highlighted dropdown.

## T3.2 Scope every read path

- Files: `actions/dashboard.go` (all handlers), templates for dropdowns/links.
- **RED** (per handler, sqlite, seeded 2 instances × distinct counts — table-driven):
  - `TestDashboardIndex_InstanceScope`
  - `TestReportsIndex_InstanceScope`
  - `TestReportsByLocation_InstanceScope`
  - `TestReportsByType_InstanceScope`
  - `TestReportsBySpecies_InstanceScope`
  - `TestConsolidatedAnimalsIndex_InstanceScopeUnified`

  Global call → totals across both; scoped call → only instance X's numbers
  (assert exact counts per status/species/type).
  `ConsolidatedAnimalsIndex` **RED** variant: `?instance_id=X` alone (without `view_mode`) must
  scope too (simplify UX — deprecate `view_mode`, keep back-compat accepting it).
- **RED** `TestReports_UnknownInstanceScoped404`: valid shape, unknown instance → 404, not an empty page.
- **RED** `TestDashboard_GlobalShowsPerInstanceCards`: global dashboard context exposes per-instance summary list (name, in-care count, last seen) for the cards.
- **GREEN**: all six handlers accept `instance_id`; dashboard builds summary from `creaves_instances` LEFT JOIN counts; instance dropdown helper renders `<select>` + JS-free form GET that keeps the param; report links include current `instance_id` (partial with plush `param` merge).
