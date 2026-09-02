# Fix Plan (closed): Reports crashing

Status: CLOSED 2026-09-02 — validated live (all report routes HTTP 200) + unit tests.

## Symptom

- `GET /reports/by_species` → HTTP 500: template iterates `results` and `years`,
  neither was ever set on the context by `ReportsBySpecies`.
- `GET /reports/by_location` → HTTP 500 (found during live validation):
  `sql: Scan error ... converting NULL to string is unsupported` on
  `location`/`postal_code`/`city` columns.
- `GET /reports` rendered unbalanced HTML: orphan "By Status" card (missing
  row/col/card wrappers), "By Year" alone, "Top Species"+"Top Cities" row.
  Same structural break in the `fr` locale variant (−3 `</div>`).

## Root causes

1. `ReportsBySpecies` (actions/dashboard.go) never called `c.Set("results")` /
   `c.Set("years")`.
2. `ReportsBySpecies` year filter: `queryArgs` missed the year bind value and the
   query was built with a double `fmt.Sprintf`.
3. `ReportsByLocation` grouped by a column that could be NULL (postal grouping
   even filtered on `discovery_city IS NOT NULL` instead of the grouping column),
   and `MAX(...)` over a NULL-able column was scanned into plain `string`.
4. `templates/reports/index.plush.html` + `index.plush.fr.html`: broken div
   nesting; all 4 `by_species.plush.*.html` had a malformed `<option>` tag
   (missing `>` before the option label, wrong selected comparison).

## Fixes

- `actions/dashboard.go`:
  - `ReportsBySpecies`: set `results`, `years` (shared `yearOption` type in
    actions/report_scope.go with `Selected` flag), fix bind args, drop the
    double Sprintf.
  - `ReportsByLocation`: WHERE filters the actual grouping column; COALESCE on
    the MAX() aggregates.
- `templates/reports/index.plush.html` rebuilt with balanced rows:
  By Status + By Year side by side, Top 20 Species + Top 20 Cities side by side.
- `templates/reports/index.plush.fr.html`: missing row/col/card wrappers added.
- `templates/reports/by_species.plush{,.de,.fr,.nl}.html`: fixed `<option>`
  markup, selection now driven by `y.Selected`.

## Tests / validation

- `actions/reports_crash_test.go` (new):
  - `TestReportsIndexStructure` — div balance must be 0, all sections present.
  - `TestReportsBySpeciesRenders` — species rows render, year dropdown populated,
    year filter actually binds (2023/center-a shows only Hérisson), unknown
    instance → 404.
  - `TestReportsByLocationNullHandling` — city set/postal NULL and postal
    set/city NULL fixtures; both groupings render 200 (reproduced both 500s).
- Live validation (buffalo dev, MySQL, admin session): `/reports`,
  `/reports/by_location?group_by=city|postal_code`, `/reports/by_type`,
  `/reports/by_species`, `/reports/by_species?year=2024[&instance_id=…]`,
  `/reports/annual` → all HTTP 200.
- Gates: `go vet ./...` clean; `staticcheck ./...` only pre-existing findings
  (ST1005 capitalized error strings in instances/users/webhook_api_keys,
  U1000 unused `scopedWhere`/`nullableString`, ST1019 double `log` import —
  all untouched by this change); `gocognit -over 15` / `gocyclo -over 12` only
  pre-existing warnings (WebhookEventsHandler, UpdateFromPayload,
  DashboardIndex, applyConsolidatedAnimalFilters, installSafePopTxLogger,
  LocalizedField, UpsertByInstanceID);
  `CGO_ENABLED=1 go test -count=1 -race -cover -tags sqlite ./...` green.

## Follow-ups noticed (not part of this fix)

- Species labels in annual/by-species dropdowns can render raw translation-map
  fragments for e2e-seeded data (`species|E2E_Hedgehog|...`) — investigate
  stored `translations` format separately if it reproduces with real payloads.
