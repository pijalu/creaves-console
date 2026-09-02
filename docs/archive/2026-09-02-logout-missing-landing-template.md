# Logout 500 (missing auth/landing template) — fixed with working logout form

Status: DONE 2026-09-02 — `GET /auth` redirects instead of erroring, and the
navbar logout is a real, CSRF-protected form that ends the session without
JavaScript; validated with unit tests + live curl.

## Original bug (bugs.md: "creaves-console: Logout returns an error")

- Logging out (or visiting `GET /auth`) returned a 500:
  `auth/landing.plush.html: could not find template auth/landing.plush.html`.
  `AuthLanding` (actions/users.go) still rendered the scaffold-era
  `auth/landing.plush.html`, which was never created.
- Root cause of the broken logout click: the navbar link relied on
  `data-method="DELETE"`, but `buffalo.js` is not loaded, so the browser just
  followed `href="/auth"` → `AuthLanding` → 500. Even with JS, the layout
  carries no CSRF meta tag, so the DELETE would not have carried a token.

## Fix

- `actions/users.go`: `AuthLanding` now redirects — signed-in users to
  `/dashboard`, everyone else to `/auth/new` (mirrors `AuthDestroy`).
- `actions/app.go`: new `POST /auth/logout` route wired to the existing
  `AuthDestroy` (the old `DELETE /auth` route stays for API completeness).
- `templates/application.plush.html` (+ de/fr/nl variants): the logout link is
  now a real `<form method="POST" action="/auth/logout">` with a hidden
  `authenticity_token` input — works without JS and is CSRF-protected.
- `actions/render.go`: `authenticity_token` plush helper resolves the token
  from the csrf middleware and returns an empty string when rendering without
  it (minimal unit-test apps mount no csrf middleware), keeping every page
  that uses the layout renderable in tests.
- `actions/users_test.go` (new): `GET /auth` → 302 `/auth/new` for anonymous
  callers; `GET /auth` → 302 `/dashboard` when signed in.

## Validation

- Unit suite: `CGO_ENABLED=1 go test -count=1 -race -tags sqlite
  ./actions/... ./models/...` ok.
- Quality gates: `go vet` clean; `staticcheck`, `gocognit -over 15`,
  `gocyclo -over 12` show only pre-existing, unrelated findings.
- Live curl (dev server :3001, MySQL `consolidation`, admin account):
  - logged-in `GET /auth` → 302 `/dashboard`.
  - anonymous `GET /auth` → 302 `/auth/new` → followed 200 (login page).
  - full UI flow: login → `POST /auth/logout` with token from the navbar form
    → 302 `/auth/new`; `GET /auth` afterwards → 302 `/auth/new` (session
    cleared).
