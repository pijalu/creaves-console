# e2e-testing (Console side)

How to run the two-app e2e validation from the console's perspective
(plan §7.2). The canonical, full version lives in the Creaves repo at
`creaves/.agents/skills/e2e-testing/SKILL.md`; this file covers the
console-specific parts.

## What this repo provides

- `buffalo task db:seed:e2e` seeds, on a fresh DB:
  - webhook API key `e2e-key` (bcrypt hash of the fixed raw key
    `e2e-console-key-0123456789` — the Creaves e2e config sends it as Bearer),
  - instance `e2e-instance-b` ("E2E Center B") with 6 fixed
    `consolidated_animals` rows: 2025 ×5 (incl. one row with all category
    fields NULL → every report bucket shows "Unknown"; one `E2EB_DCD` dead
    outtake; one city `E2EB City; "Sud"` for CSV escaping), 2024 ×1.
- Instance A (`e2e-instance-a`) is **not** seeded here: its 9 rows must arrive
  through the real webhook path (Creaves resync → `POST /webhook/events`).

## Bring-up

```sh
cd creaves-console
buffalo pop drop -e development && buffalo pop create -e development \
  && buffalo pop migrate -e development
buffalo task db:seed && buffalo task db:seed:e2e
nohup buffalo dev > /tmp/console-dev.log 2>&1 &
# login: admin / admin123
```

Then bring up Creaves (see its skill) and trigger a resync so instance A rows
arrive. Expected console contents afterwards:

```sql
SELECT instance_id, COUNT(*) FROM consolidated_animals GROUP BY instance_id;
-- e2e-instance-a = 9, e2e-instance-b = 6
```

## Console e2e assertions (agent-browser)

- `/consolidated_animals` — new filters (entry cause, age, ring, outtake
  type); exact row sets from `creaves/e2e/EXPECTATIONS.md`.
- Export CSV with filters + scope; assert escaping of `E2EB City; "Sud"`.
- `/reports/annual?year=2025` — scopes:
  - all centers (default): totals = A + B,
  - `instance_id=e2e-instance-a`: A only,
  - `instance_id=e2e-instance-b`: B only.
  Note: the console has **no error-outtake exclusion** (the payload carries no
  error flag), so A-scope outtake tables include `E2E_ERR` (T=4 for 2025).
- Annual export CSV per scope.
- 4 languages (fr, en-US, de, nl): spot-check labels + navbar Reports menu in
  all 4 variants. Fixture values with stored translations render localized
  (e.g. `E2E_Hedgehog` → `species|E2E_Hedgehog|creaves_species|DE` under de);
  untranslated values fall back to the canonical French string.

## Gotchas

- Tests here run on SQLite: `CGO_ENABLED=1 go test -tags sqlite ./...`.
- Dev DB is MySQL (`consolidation`), same credentials as Creaves
  (`creaves`/`creaves`) — qualify table names when both apps are up.
- `CONFIRM=cleanup buffalo task db:cleanup` wipes data without touching
  migration history.
