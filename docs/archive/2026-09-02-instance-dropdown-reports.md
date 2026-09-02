# Instance selection dropdown on all report pages

Status: DONE 2026-09-02 — all report pages select the instance via a dropdown;
validated with unit tests + live curl.

## Original bug (bugs.md: "creaves-console: Reports — Instance selection should be a dropdown")

- `/reports` used a free-text input for `instance_id` (typo-prone, no list of
  known instances).
- `/reports/by_location`, `/reports/by_type`, `/reports/by_species` carried the
  instance only as a hidden parameter (or not at all), so the scope could not be
  changed from those pages.
- The `de`/`nl` locale variants of the reports index had no instance selector.

## Fix

- `actions/dashboard.go`: `ReportsIndex`, `ReportsByLocation`, `ReportsByType`,
  `ReportsBySpecies` now load the instance list via `listAnnualInstances`
  (reusing the `/reports/annual` dropdown pattern) and expose it as `instances`.
- Templates (`templates/reports/`, all locales en/fr/de/nl): the instance filter
  is now a `<select name="instance_id">` dropdown with "All centers" plus one
  `Name (ID)` entry per instance, auto-submitting on change (noscript fallback
  button). `by_location` keeps `group_by` in a hidden input; `by_species` keeps
  its year filter in the same form. Unknown instances still 404 via
  `reportScope`.

## Validation

- `TestReportsInstanceDropdownOnAllPages` (actions/reports_crash_test.go):
  dropdown present on `/reports`, `/reports/by_location`, `/reports/by_type`,
  `/reports/by_species`; selection persists across pages; unknown instance 404s.
- Full suite: `CGO_ENABLED=1 go test -tags sqlite ./actions/... ./models/...`.
- Live curl (dev server, admin session): all report routes HTTP 200 with the
  dropdown rendered.
