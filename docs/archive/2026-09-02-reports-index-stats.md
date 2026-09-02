# Reports index stats: readability, layout balance, missing in-care stat

Status: DONE 2026-09-02 — validated with unit test + full suite + live curl.

## Original bug (bugs.md: "creaves-console: Reports")

- The number of animals was unreadable: `.stat-number` hard-coded
  `color: #007bff` while the card was `bg-primary` (blue on blue) and the single
  stat card took a disproportionate amount of space.
- The stats grid was unbalanced: one lone stat card, then "By Status" centered
  with "By Year" alone on the left, then "Top 20 Species" and "Top 20 Cities"
  together.
- The "Animaux en cours de soins" (animals in care) stat was missing; it must
  match the all-centers vs selected-instance scope.

## Fix

- `templates/application.plush{,.fr,.de,.nl}.html`: `.stat-number` now uses
  `color: inherit`, so it renders white on `bg-primary` cards and normal text
  color on plain cards.
- `actions/dashboard.go` (`ReportsIndex`): extracted `statusCount` /
  `tallyStatusCounts` and exposes `stats["in_care"]`, `stats["released"]`,
  `stats["died"]` from the existing grouped status query (respects
  global/instance scope through the same `where` clause).
- `templates/reports/index{,.fr,.de,.nl}.html`: the stats row is now a balanced
  4-column row — Total Animals / Animals in Care / Released / Died (localized:
  fr "Animaux au total / Animaux en cours de soins / Relâchés / Décédés", de
  "Tiere gesamt / Tiere in Pflege / Ausgewildert / Verstorben", nl "Totaal
  aantal dieren / Dieren in zorg / Uitgezet / Overleden").

## Validation

- Unit test `actions/reports_index_stats_sqlite_test.go`
  (`TestReports_IndexStats`): seeds 2 centers, asserts global counts (4/3/1/0)
  and instance-scoped counts (3/2/1/0) rendered, plus `color: inherit` present
  and no `#007bff` left.
- Full suite: `CGO_ENABLED=1 go test -tags sqlite ./actions/... ./models/...`
  green.
- Quality gates: `go vet`, `staticcheck` (no new findings), `gocognit`,
  `gocyclo` — `ReportsIndex` no longer exceeds thresholds (helper extraction).
- Live curl (dev server, admin session): `/reports` global 15/7/8/0;
  `/reports?instance_id=e2e-instance-a` 9/4/5/0 with dropdown selected; CSS
  served as `color: inherit`; all four locales render their localized labels.
