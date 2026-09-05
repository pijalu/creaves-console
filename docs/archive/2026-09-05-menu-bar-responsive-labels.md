# Fix plan — Menu bar: hide labels before collapsing to hamburger

Date: 2026-09-05
Source: workspace `bugs.md` → "creaves-console: menu bar"

## Bug report

The menu bar should remove the label when there is no space to put them next to
the icons. And only collapse to a hamburger menu when there is no space for the
icons themselves.

## Root cause

`templates/application.plush.html` (and the `de`, `fr`, `nl` locale variants)
uses `navbar navbar-expand-lg`: Bootstrap 4.6 collapses the whole navbar into a
hamburger below 992px. There is no intermediate state: labels are never dropped
independently — the menu jumps straight from full "icon + label" to hamburger
even when the icons alone would still fit comfortably.

## Change

All four layout variants get the same structural change (only translations
differ between them):

1. `navbar-expand-lg` → `navbar-expand-sm`: the navbar only collapses into the
   hamburger below 576px, i.e. when there is genuinely no room for the icon row.
2. Every nav link / dropdown toggle / navbar-text label is wrapped in
   `<span class="nav-label d-none d-lg-inline">…</span>`: labels disappear below
   992px (icons remain visible and functional), and reappear from 992px up.
3. Small CSS addition so labels remain visible inside the *expanded hamburger*
   menu (below 576px the collapse panel is vertical and has plenty of room;
   hiding labels there would make the menu unusable):
   `@media (max-width: 575.98px) { #navbarNav .nav-label { display: inline !important; } }`

Resulting behavior:

- ≥992px: icons + labels (unchanged).
- 576–991px: icons only (labels hidden), no hamburger.
- <576px: hamburger; opened menu shows icons + labels stacked vertically.

## Test approach

- Template-only change; no Go logic touched. Existing Go test suite must still
  pass (`CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...`).
- Browser validation with the agent-browser skill against `buffalo dev`
  (http://localhost:3001, login admin/admin123):
  - viewport 1280px: labels visible, no hamburger.
  - viewport 768px: labels hidden, icons visible, no hamburger.
  - viewport 400px: hamburger visible; opening it shows labels next to icons.
  - spot-check one non-English locale (fr) renders the same structure.

## Validation steps

1. `buffalo dev` starts without template errors.
2. Browser checks above pass at the three viewport widths.
3. Quality gates (each run separately): `go vet ./...`, `staticcheck ./...`,
   `gocognit -over 15 .`, `gocyclo -over 12 .`,
   `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...`.
4. Commit; move bug + this plan to `docs/archive/`; remove the section from
   workspace `bugs.md`.

## Validation results (2026-09-05)

Browser validation with agent-browser (authenticated as admin, `/dashboard`):

| Viewport | Hamburger | Labels | Result |
|----------|-----------|--------|--------|
| 1280px | hidden | 7/7 visible | OK — full icon + label bar |
| 768px | hidden | 0/7 visible | OK — icon-only bar, no hamburger |
| 400px | visible, opened via click | 7/7 visible in open menu | OK — hamburger collapse with labels |

French locale (`/lang/?lang=fr&url=/dashboard`) spot-checked: 768px → 0/7
labels, no hamburger; 1280px → 7/7 labels. Same behavior as English.

Quality gates (each run separately, creaves-console):

- `go vet ./...` — clean.
- `staticcheck ./...` — clean.
- `gocognit -over 15 .` — 5 pre-existing warnings (UpdateFromPayload,
  installSafePopTxLogger, TestOutcomeHelpersAcceptValueAndPointer,
  DashboardIndex, SyncManagementIndex); all unrelated to this template-only
  change.
- `gocyclo -over 12 .` — 8 pre-existing warnings (same functions plus
  LocalizedField, applyConsolidatedAnimalFilters, UpsertByInstanceID,
  EventsDeleteCreate); all unrelated to this change.
- `CGO_ENABLED=1 go test -tags sqlite -count=1 -race -cover ./...` — PASS
  (actions 59.5%, models 58.5%).
