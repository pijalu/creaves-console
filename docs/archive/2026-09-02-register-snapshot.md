# Yearly register + register snapshot (per instance)

Status: DONE 2026-09-02 — `/reports/register` and `/reports/snapshot` added to
creaves-console, mirroring the Creaves `/registertable` and `/registersnapshot`
pages; validated with unit tests + live curl.

## Original request (bugs.md: "creaves-console: yearly register + register snapshot")

- The console had no per-year register of all animals and no point-in-time
  snapshot of animals in care — both exist in the Creaves main tool
  (`/registertable`, `/registersnapshot`) but not over the consolidated view.
- Both needed to be scoped per instance (all centers or a single center) like
  the existing reports, with CSV export, readable yearly totals and working
  filters.

## Implementation

- `actions/register_reports.go` (new):
  - `RegisterIndex` (GET `/reports/register?year=YYYY&instance_id=…`): one
    register year, paginated, year + center dropdowns (latest year selected by
    default), total count alert, "Empty database" guard. Rows link to the
    consolidated animal detail. Center column only in global scope.
  - `RegisterExportCSV` (GET `/reports/register/export.csv`): all rows of the
    year, `registertable-YYYY[-instance].csv`, UTF-8 BOM + `;` separator,
    localized header (`registerCSVHeader`), leading Instance column.
  - `SnapshotIndex` (GET `/reports/snapshot?snapshotDate=YYYY-MM-DD&instance_id=…`):
    animals present in care at a date (intake strictly before day+1, no
    outtake or outtake on/after the day — mirrors the Creaves query), capped
    at 2000 rows like the original, total + cap warning. Native
    `<input type="date">`, defaults to today; also accepts the Creaves-style
    `YYYY/MM/DD`.
  - `SnapshotExportCSV` (GET `/reports/snapshot/export.csv`):
    `registersnapshot-YYYY-MM-DD[-instance].csv`.
  - Instance scoping via `reportScope` (unknown instance → 404), dropdown via
    the existing `listAnnualInstances`, years via the existing
    `selectAnnualYear`.
- `actions/app.go`: 4 new routes under `/reports`.
- Templates `templates/reports/register.plush.{html,fr,de,nl}.html` and
  `snapshot.plush.{html,fr,de,nl}.html` (4 locales each).
- Navbar Reports dropdown gained "Register" / "Register snapshot" in all 4
  `application.plush.*.html` layouts.

## Validation

- Unit tests (actions/register_reports_sqlite_test.go, sqlite tag):
  page rendering, default/latest year, year + instance filters, global vs
  scoped totals, empty-database guard, unknown instance 404, CSV headers +
  filenames + row sets, snapshot boundary semantics (intake day included,
  outtake day still present, day after outtake excluded), invalid date 500,
  Creaves-style date accepted.
- Full suite: `CGO_ENABLED=1 go test -tags sqlite ./actions/... ./models/...`
  ok; `-race` ok.
- Quality gates: `go vet`, `staticcheck` (pre-existing findings only),
  `gocognit -over 15`, `gocyclo -over 12` (no new findings).
- Live curl (dev server on :3001, MySQL `consolidation` DB, admin session):
  - Register global: latest year 2025 auto-selected, "Total animals for 2025:
    12"; year=2024 + instance filter narrows correctly (1 for
    e2e-instance-b).
  - CSV: `registertable-2024.csv` served with BOM header + per-instance rows;
    scoped filename `registersnapshot-2025-06-15-e2e-instance-a.csv`.
  - Snapshot counts cross-checked against equivalent SQL: global 8 = SQL 8,
    e2e-instance-b 0 = SQL 0.
