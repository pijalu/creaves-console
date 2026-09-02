# Instance shown for each animal in the console

Status: DONE 2026-09-02 — the register list, show page and drill-down all
display the animal's source instance; validated with unit tests + live curl.

## Original bug (bugs.md: "The console should show for each animal the instance it belongs to")

- `templates/consolidated_animals/index.plush.html` (all four locales) had no
  Instance column in the register table, so animal rows could not be
  attributed to their center.
- The show page (`show.plush.html`) and drill-down (`drill_down.plush.html`)
  already rendered `animal.InstanceID`, and the CSV export already led its
  header with `Instance` — only the register table was missing it.

## Fix

- `templates/consolidated_animals/index.plush.html` (+ `de`/`fr`/`nl`
  variants): added an `Instance` column (`<%= animal.InstanceID %>`) as the
  first table column, matching the CSV export header order. Localized headers:
  Instance / Instance / Instanz / Instantie.
- `actions/dashboard_test.go`: `newDashboardTestApp` now also mounts
  `ConsolidatedAnimalShow` and `ConsolidatedAnimalDrillDown`; new tests
  `TestConsolidatedAnimalsRegisterShowsInstance` (Instance header present,
  both seeded instances rendered, column ordered before Year) and
  `TestConsolidatedAnimalShowAndDrillDownShowInstance` (both pages display
  the animal's instance).

## Validation

- Unit suite: `CGO_ENABLED=1 go test -tags sqlite ./actions/... ./models/...`
  ok; `-count=1 -race -cover` ok.
- Quality gates: `go vet` clean; `staticcheck`, `gocognit -over 15`,
  `gocyclo -over 12` report only pre-existing, unrelated findings (ST1005 in
  users/instances/webhook_api_keys, U1000, ST1019, complexity of
  WebhookEventsHandler/UpdateFromPayload/DashboardIndex).
- Live curl (dev server :3001, admin session, MySQL `consolidation` with
  `e2e-instance-a` (9) / `e2e-instance-b` (6) animals):
  - `GET /consolidated_animals` → 200, `<th>Instance</th>` once, 9×
    `<td>e2e-instance-a</td>`, 6× `<td>e2e-instance-b</td>`.
  - Show + drill-down of a record → 200, instance displayed.
  - Locale variants via `/lang/?lang=…`: en/de/fr/nl all 200 with the
    localized Instance/Instanz/Instantie header.
