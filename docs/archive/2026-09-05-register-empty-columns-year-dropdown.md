# bugs.md item 5 — Animals register: empty columns + year dropdown all-selected (CRITICAL UX)

Archived: 2026-09-05 · Repo: creaves-console · Status: fixed, tested, browser-validated

## Observed

`/consolidated_animals` (Animal Register) rendered:

1. **Empty columns** — Species, Identification (ring), Discovery location, PC, City and
   Cause rendered blank for **every** row, although the same values show on the detail
   page. Root cause: the six cells used Plush *inline* if-expressions

   ```html
   <td><%= if (animal.Ring.Valid) { animal.Ring.String } else { "-" } %></td>
   ```

   Plush parses the inline form but never *prints* its expression; only the block form
   (`<%= if (cond) { %>…<% } else { %>…<% } %>`) emits output. The Intake date/time and
   Outcome cells already used the block form and rendered fine.

2. **Year dropdown marked every option `selected`** — the option template compared

   ```html
   <%= if (params["year"] == string(y.Year)) { %>selected<% } %>
   ```

   In Plush `string(int)` yields `""`, so for the default page (`params["year"]` empty)
   the condition was **always true** (browser displayed the last option, 2021, while the
   actual filter was "All"); when a year *was* selected, `"" != "2024"` made the
   condition always false and **no** option was marked, so the browser silently showed
   "All" while filtering by that year.

## Fix

- `actions/dashboard.go` (`ConsolidatedAnimalsIndex`): build `yearsList` as
  `[]string` (years formatted with `strconv.Itoa`) so the template compares string to
  string — no Plush `string(int)` cast anywhere.
- `templates/consolidated_animals/index.plush{,.de,.fr,.nl}.html`:
  - expanded all six inline ifs (Species, Identification, Discovery location, PC, City,
    Cause) to the block form used by the working cells;
  - year options now `params["year"] == y` over the `[]string` `yearsList`.

## Tests (fail-first verified)

New `actions/consolidated_animals_render_test.go` (sqlite tag), rendering the real page
through the handler:

- `TestConsolidatedAnimalsRegisterRendersNonEmptyCells` — species, ring, discovery
  location, postal code, city and entry cause appear in the rendered table; rows with
  NULL values render `-` (was: empty `<td></td>`).
- `TestConsolidatedAnimalsYearDropdownExactlyOneSelected` — default page marks no year
  option selected (browser falls back to "All"); `?year=2024` marks **exactly one**
  option selected and it is 2024 (was: all selected / none selected).

Both tests were run against the unfixed code and failed exactly as the bug describes
(RED), then passed after the fix (GREEN).

Fixtures: `seedRegisterFixtures` gained discovery-location/postal-code/city columns
(kept NULL for absent values via new `optionalString` helper); complexity kept ≤15
(gocognit).

## Validation

- Browser (agent-browser, dev server :3001, real data):
  - EN `/consolidated_animals`: rows render Species/Identification/Location/PC/City/
    Cause with values and `-` fallbacks (e.g. `lagrange 2026 6 Hérisson - … Rue De Frise,
    72 5310 Mehaigne Pest control trapping`).
  - `?year=2025` → `select#year` contains exactly one `selected` (2025); default page →
    zero `selected` (All is the effective default).
  - DE: same column rendering with localized labels; FR and NL: `?year=2024` /
    `?year=2023` each mark exactly one option selected.
- `go vet ./...` clean; `staticcheck ./...` clean;
  `gocognit -over 15` / `gocyclo -over 12` — remaining findings are pre-existing and in
  code untouched by this fix (DashboardIndex, SyncManagementIndex,
  TestOutcomeHelpersAcceptValueAndPointer, applyConsolidatedAnimalFilters,
  installSafePopTxLogger, UpsertByInstanceID, EventsDeleteCreate).
- `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...` green
  (actions 57.2%, models 59.1% coverage). gofmt drift in
  `actions/event_processor_poison_test.go` / `actions/reports_annual.go` is pre-existing
  and untouched.
