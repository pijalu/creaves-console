# Webhook API key delete, confirm dialog, and key visibility

Status: DONE 2026-09-04 — delete works from index + show via inline POST
forms with confirm dialog; documented key identifier shown on both pages;
raw key remains one-time only (bcrypt); validated with Go tests.

## Original bug (bugs.md "creaves-console")

- Delete on the show page did nothing: the link relied on
  `data-method="DELETE"`, but no JS handler for `data-method` exists in the
  app (application layout only loads jQuery + Bootstrap, no buffalo.js /
  UJS shim), so the click just followed a GET to the show page.
- Index delete button opened the view but had no working confirmation +
  delete path for the same reason.
- "The view should show the API key value": only the `creaves_<prefix>…`
  identifier can be displayed. The raw key is bcrypt-hashed at creation and
  shown exactly once on the dedicated created page (see archive
  2026-09-02-api-key-created-page.md); it is cryptographically
  unrecoverable afterwards, so index/show document the identifier plus a
  note pointing at the one-time page instead of pretending to redisplay it.

## Implementation

- `templates/webhook_api_keys/index.plush.{html,fr,de,nl}.html`: replaced the
  dead `data-method="DELETE"` anchor in each row with an inline form
  `POST /webhook_api_keys/<id>` carrying `authenticity_token` (CSRF) +
  `_method=DELETE` (buffalo MethodOverride) + `onsubmit="return
  confirm('…')"` localized per locale. No JS dependency.
- `templates/webhook_api_keys/show.plush.{html,fr,de,nl}.html`: same inline
  delete form in the card footer with localized confirm text; added an
  "API key value" row documenting the one-time display (identifier
  `creaves_<prefix>…` + note that the raw value is only shown once after
  creation because only a bcrypt hash is stored).
- `actions/webhook_api_keys_test.go` (sqlite tag):
  - `TestWebhookAPIKeys_ShowHTML`: now also asserts prefix + "API key
    value" note + `_method=DELETE` + `authenticity_token` + `confirm(`.
  - New `TestWebhookAPIKeys_ListHTMLDeleteForm`: index HTML contains the
    inline DELETE form with CSRF token + confirm.
  - New `TestWebhookAPIKeys_DestroyViaMethodOverride`: `POST
    /webhook_api_keys/<id>` with `_method=DELETE` → 303 and row deleted
    (covers the real browser path, no JS).

## Validation

- `CGO_ENABLED=1 go test -tags sqlite -count=1 ./actions/ ./models/` → ok
  (both packages).
- Targeted: `-run
  'TestWebhookAPIKeys_ShowHTML|TestWebhookAPIKeys_ListHTMLDeleteForm|TestWebhookAPIKeys_DestroyViaMethodOverride|TestWebhookAPIKeys_Destroy$'
  ./actions/ -v` → all PASS; method-override request logged as
  `method=DELETE … status=303` and count drops to 0.
- Template grep: each of the 8 index/show templates (en/fr/de/nl) contains
  exactly one `_method" value="DELETE"` form; zero `data-method` remains
  under `templates/webhook_api_keys/`.
- `go vet ./actions/ ./models/` clean.
- Scope note: `templates/users/index.*` still uses `data-method="DELETE"`
  (same dead pattern) — out of scope for this fix, same treatment applies
  when that section is addressed.
