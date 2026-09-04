# Instance show page 500

## Fix plan

- Reproduce `/instances/:instance_id` in English and a translated locale with an authenticated admin.
- Remove locale-template dependency on the English template when that dependency is not available to Plush's embedded renderer.
- Add regression coverage for every instance show locale.
- Validate with agent-browser and the focused SQLite test suite.

## Root cause

The French, German, and Dutch instance show templates called `partial("instances/show.plush.html")`. In the embedded render path, this partial lookup was not available, so localized instance show requests returned HTTP 500. The locale templates were changed to contain the page markup directly, matching the English template.

## Validation

- `CGO_ENABLED=1 go test -tags sqlite -count=1 ./actions -run 'InstanceShowLocale|Instance'` — passed.
- agent-browser authenticated as admin:
  - `GET http://127.0.0.1:3001/instances/LaGrange` — HTTP 200; rendered instance details.
  - switched to German through `/lang/?lang=de&url=/instances/LaGrange`, then same URL — HTTP 200; rendered instance details.
- Added `TestInstanceShowLocaleTemplatesAreSelfContained`, checking embedded English, French, German, and Dutch templates contain instance markup and no missing partial call.
