# Consolidated register: column sorting, outcome badge, outcome wording, multilingual data display

**Date:** 2026-09-05
**Status:** Implemented
Scope: this repo (receiver side). The producer-side counterpart (creaves list
sorting, Outcome rename, rating badge) is documented in
`creaves/docs/archive/animal-sort-outcome-rename-rating-badge.md`.

## Goal

1. The consolidated animals register (`/consolidated_animals`) is sortable by
   any table column, including the CSV export.
2. Animals show the outcome classification (positive / neutral / negative) as a
   badge on the index and as structured rows on the show page.
3. UI wording renames "Outtake" to "Outcome" (en), Ausgang (de),
   Issue (fr), Afloop (nl).
4. Localized (multilingual) values pushed by creaves are actually displayed:
   the outtake-type cell renders the language-specific translation with
   fallback to the canonical value.

## Design

### Column sorting (`actions/animal_sort.go`)

- `consolidatedSortColumns` is a whitelist from the public `sort` parameter to
  a fixed ORDER BY fragment (`instance_id`, `year`, `year_number`, `species`,
  `ring`, `intake_date`, `discovery_*`, `entry_cause`, `current_status`,
  `outtake_type/date/location`). The user parameter is only a map key —
  arbitrary input can never reach the SQL.
- `consolidatedSortClauses(sortKey, dir)` is a pure function returning the
  clause list; default is `year desc, year_number asc` plus a stable
  tie-breaker so paging is deterministic. `applyConsolidatedSort` applies it in
  `ConsolidatedAnimalsIndex` **and** `ConsolidatedAnimalsExportCSV`, so the
  export honours the same ordering as the table.
- `sortLink`/`sortIcon` plush helpers: toggle direction on the active column,
  preserve filters, drop `page`; ▲/▼ marks the active sort.

### Outcome classification (`models.ConsolidatedAnimal.OutcomeStatus`)

Value receiver, no DB access:

- `outtake_dead == true` → `negative` (always wins)
- `outtake_rating < 0` → `negative`, `> 0` → `positive`, `== 0` → `neutral`
- no dead flag and no rating → `""` (unknown; no badge rendered)

Helpers in `actions/animal_sort.go` render it: `outcomeClass` maps to
`badge-success/secondary/danger`, `outcomeLabel` to localized labels
(en Negative/Neutral/Positive, fr Négative/Neutre/Positive, de
Negativ/Neutral/Positiv, nl Negatief/Neutraal/Positief). Both accept the value
form (index loop variable) and the pointer form (show handler) — plush passes
them differently — and error out on unrelated types instead of guessing.

### Templates

- index: 13 sortable headers per language; outcome badge next to the status
  badge (hidden when unknown); outtake-type cell now uses
  `tfield_localized(animal, "outtake_type")` so the pushed translations map is
  used; filter label renamed to Outcome type:/Ausgangsart:/Type d'issue
  :/Afloop type:.
- show: new rows for Outcome (badge + type), Outcome date and Outcome
  location, each gated on the corresponding stored value; unknown outcome
  renders no row.
- drill_down + reports register: renamed Outtake date/location/reason labels.

## Testing

- `actions/animal_sort_test.go`
  - `TestConsolidatedSortClausesPinsDefaultOrdering` — no/unknown/invalid
    `sort` (incl. an injection attempt) falls back to the default ordering.
  - `TestConsolidatedSortClausesPinsWhitelistAndDirection` — whitelist and
    direction handling for every key.
  - `TestOutcomeStatusPinsClassification` — dead beats a positive rating,
    sign rules, unknown when nothing stored.
  - `TestConsolidatedSortHelpersRender` — sortLink toggle/preserve/reset and
    sortIcon output.
  - `TestOutcomeHelpersAcceptValueAndPointer` — helpers accept both the value
    and pointer form of the model and reject unrelated types with an error.
- `models/consolidated_animal_test.go`:
  `TestApplyEvent_AllLocalesRoundTrip` pushes one event carrying the
  translations map for all four languages and asserts `LocalizedField` returns
  each language's value — multilingual export→import round trip.
- `actions/dashboard_test.go` register test updated for the now-sortable
  header markup (asserts `sort=instance` link, not a plain `<th>`).
- Full suite green: `CGO_ENABLED=1 go test -tags sqlite ./actions/... ./models/...`
  (SQLite test DB; also run untagged for the pure tests).

## Validation (dev servers)

- Console (port 3001): index, show and drill_down return 200 in en-US/fr/de/nl;
  `Species ▲` indicator; fr shows `Type d'issue :` and badges
  `badge-danger">Négative` / `badge-success">Positive`; the outtake-type cell
  switches language with the lang cookie (fr: Relâché/Euthanasié… vs en:
  Released/Euthanized…) proving the stored translations are displayed; show
  pages render the Outcome/Ausgang/Date d'issue/Afloopdatum rows only when the
  data exists.
- Producer-side checks are listed in the creaves archive doc.
- All 13 changed plush templates parse cleanly (plush Parse-only sweep).
