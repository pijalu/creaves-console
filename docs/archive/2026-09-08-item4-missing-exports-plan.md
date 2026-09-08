# Item 4 — Port the 6 portable missing exports to creaves-console: fix plan

Date: 2026-09-08
Status: **done** — implemented, tested, e2e-validated, committed.
Investigation: [item4-missing-exports-investigation.md](item4-missing-exports-investigation.md)
(same directory) — full inventory + reasons for the non-portable exports.

## Scope

Port the 6 portable Creaves exports missing from the console
(`/export/reports`, online view + CSV, instance-scoped):

- Date aggregates over `consolidated_animals.intake_date` (all `Aggregate: true`):
  - `entry_date_year` — animals per day of the year
  - `entry_day_week` — animals per weekday (MySQL DAYOFWEEK convention, 1=Sunday)
  - `day_to_month` — animals per month
- Annexe (subsidy) reports:
  - `Annexe_2A_2024` — detail rows for SG1/SG2/SG3 subside groups with the
    DCD / Relacher / Transferer / Adoption mapping on outtake type names
  - `Annexe_2B_2024` — counts per year × subside group
  - `Annexe_2024` — counts per year × class/order grouping
    (Oiseaux / Mammifères non volants / Mammifères volants et autres espèces)

Not ported (by design, reasons in the investigation doc):
`treatments_day_year`, `animal_gavage`, `controle_espèce` — they require data
that is not part of the webhook payload.

## Implementation

`actions/export_queries.go`
- 6 new entries in the `exportQueries` registry (index/view/CSV handlers pick
  them up automatically).
- Annexe queries approximate the Creaves originals: `outtaketypes.error` is
  not in the webhook payload, so the `oo.error = 0 OR oo.error IS NULL` filter
  is dropped and the approximation is documented in each Description. WHERE
  clauses use explicit parentheses (the Creaves originals rely on MySQL
  AND/OR precedence — the precedence bug is not replicated).
- `Annexe_2A_2024`/`Annexe_2B_2024` filter on
  `a.species_subside_group IN ('SG1','SG2','SG3')`.
- `Annexe_2024` uses equivalent-semantic IN-list CASE (WHEN chains collapse
  to the same branches).

`actions/export_reports.go`
- New placeholders for the date aggregates:
  - `{year:col}` → `YEAR(col)` (MySQL) / `strftime('%Y', col)` (sqlite)
  - `{dow:col}` → `DAYOFWEEK(col)` (MySQL) /
    `(CAST(strftime('%w', col) AS INTEGER) + 1)` (sqlite; %w is 0=Sunday)
- Doc comment on the placeholder list fixed (it documented `{df}`/`{year}`
  with a syntax the implementation never had).

## Test approach

`actions/export_reports_sqlite_test.go` (sqlite build tag, real sqlite engine):
- Registry test updated: 28 queries.
- `TestExportReports_DateAggregates` — entry_date_year / entry_day_week /
  day_to_month rows, weekday numbering (Wed=4, Fri=6), month counts, and the
  instance-scope filter.
- `TestExportReports_AnnexeReports` — dedicated fixture spanning all subside
  groups and class/order branches (+ unknown species, + non-SG animal):
  2A detail rows + group labels + DCD/Relacher mapping + scope filter;
  2B per-group counts; Annexe_2024 per-group counts + scope filter.
- `TestExportReports_AllQueriesRunOnSQLite` automatically covers the 6 new
  queries (global + scoped, CSV path).
- Item-1 Excel regression criterion: the existing excel sqlite tests
  (registre_detail / stat_communes pivot-cache invariants) keep passing in
  the full-suite run below.

## Validation results (2026-09-08)

- `CGO_ENABLED=1 go vet -tags sqlite ./...` — clean.
- `staticcheck ./...` — clean.
- `gocognit -over 15 .` — 4 warnings, all pre-existing and unrelated
  (models.UpdateFromPayload, models.installSafePopTxLogger,
  animal_sort_test helper, SyncManagementIndex).
- `gocyclo -over 12 .` — 7 warnings, all pre-existing and unrelated (same
  functions + a few more; none in the touched files).
- `CGO_ENABLED=1 go test -count=1 -race -cover -tags sqlite ./...` —
  `ok creaves-console/actions 64.4s (56.3%)`, `ok creaves-console/excel`,
  `ok creaves-console/models`. The actions run includes the excel sqlite
  tests (item-1 criterion: no Excel repair warnings) — all green.

## E2E evidence (bugs.md guideline 5/6 — agent-browser skill)

Dev server: `buffalo dev` (MySQL `consolidation`, 10046 consolidated animals,
instance LaGrange), login admin / admin123.

- `agent-browser open http://127.0.0.1:3001/export/reports` →
  `snapshot -c` shows all 6 new reports listed:
  `entry_date_year` (e70), `entry_day_week` (e73), `day_to_month` (e76),
  `Annexe_2A_2024` (e79, with approximation description), `Annexe_2B_2024`
  (e82), `Annexe_2024` (e85).
- Online view: `open /export/reports/view?query=Annexe_2B_2024` →
  "Annexe_2B_2024 — 18 row(s)", per-year A)/B)/C) rows 2021–2024 with real
  counts (e.g. 2024 A) Mammifères non volants (100/tranche) 1103).
- Online view: `/export/reports/view?query=entry_day_week` renders per-year
  weekday rows (2021 1 160 …).
- CSV downloads (browser download, `Content-Disposition: attachment`):
  all 6 files landed in `~/Downloads`:
  - `entry_day_week.csv` — header
    `Année;numéro jour de la semaine - (1=dimanche);Nombre`, rows `2026;1;252 …`
  - `entry_date_year.csv` — `Année;Date d'Entrée;Nombre`, `2026;2026 01 01;2 …`
  - `day_to_month.csv` — `Année;Mois d'Entrée;Nombre d'Animaux`, `2026;01;55 …`
  - `Annexe_2A_2024.csv` — detail rows with Groupe labels and
    `Raison de la sortie` = DCD (mapped from "Rat brun" outtake), Instance column
  - `Annexe_2B_2024.csv` — per-year × group counts
  - `Annexe_2024.csv` — `année;Rapport Groupe;Nombre`, `2026;Oiseaux;992 …`
- Browser session closed, dev server stopped.
