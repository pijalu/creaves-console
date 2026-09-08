# Item 4 — Creaves-Console missing exports: investigation

Date: 2026-09-06
Source request (bugs.md): "Quite a few exports seems to be missing in the console:
Annexe(s), Registre, Age à l'entrée, Causes de sortie, animal per day, per week,
per month… By default, an export in creaves should be available in the console —
if it is not, please provide a clear reason why."

## Method

Full inventory of Creaves exports (three mechanisms):

1. `creaves/export/config.yaml` — 32 generic SQL reports, served at
   `/export/csv` (CSV download) and `/export/view` (online HTML).
2. `creaves/excel/config/config.yaml` — 2 template-based Excel workbooks at
   `/export/excel` (registre_detail, stat_communes).
3. Hard-coded routes in `creaves/actions/app.go`:
   `/registertable/ExportCSV`, `/registersnapshot/ExportCSV`,
   `/reports/annual/export.csv`, `/animals/search/export.csv`.

Console side, four surfaces:

- `/reports/csv` (nav "Exports") — hub page: Excel links, Export Reports card
  (→ `/export/reports`, 22 ported reports), Consolidated Animals CSV,
  Register Table CSV, Year-End Snapshot CSV, Annual Report CSV.
- `/export/excel?query=registre_detail|stat_communes` — both Creaves Excel
  exports ported (Bug 6), scope-aware (all centers / one center).
- `/export/reports` (+ `/view`, `/export.csv`) — 22 ported reports running on
  `consolidated_animals` (bugs.md item 3), incl. **Registre**, **Registre
  détaillé**, **Age à l'entrée** (`entry_age`), **Causes de sortie**
  (`sortie_reason`), dead/discoverer/donation/species registers, all species
  breakdowns, entry-cause breakdowns, `nombre`.
- `/consolidated_animals/export.csv`, `/reports/annual/export.csv`,
  `/reports/register/export.csv`, `/reports/snapshot/export.csv` — counterparts
  of the hard-coded Creaves routes (animal-search export is the only Creaves
  route without a console counterpart; it is a search-result dump, superseded
  by the Consolidated Animals filterable view + CSV).

## User-named exports: actual status

| User-named export | Console status |
|---|---|
| Registre | **Present** — `register`, `detail_register` in `/export/reports`; Excel `registre_detail` in `/reports/csv`; also Register Table report. |
| Age à l'entrée | **Present** — `entry_age` in `/export/reports` (online view + CSV). |
| Causes de sortie | **Present** — `sortie_reason` in `/export/reports`; also `sortie_types`. |
| Annexe(s) | **Missing** — Annexe_2A_2024, Annexe_2B_2024, Annexe_2024 not ported. |
| animals per day / week / month | **Missing** — `entry_date_year`, `entry_day_week`, `day_to_month` not ported. |

Note: the "present" ones were added by item 3 and are reachable via
nav → Exports (`/reports/csv`) → "Export Reports" card → `/export/reports`.
If the user could not find them, that is a discoverability issue, not a
missing feature.

## Missing exports and reasons

### Missing, portable (data already consolidated) — implementation candidates

| Creaves export | What it needs | Feasibility |
|---|---|---|
| `entry_date_year` (animals per day of year) | `intake_date` | **Trivial port** — aggregate on `consolidated_animals.intake_date`. |
| `entry_day_week` (per weekday) | `intake_date` | **Trivial port** — DAYOFWEEK/strftime('%w'). |
| `day_to_month` (per month) | `intake_date` | **Trivial port** — month extraction already supported by placeholders. |
| `Annexe_2A_2024` | `species_subside_group` (consolidated ✓), outtake type name + `outtaketypes.error` flag | **Partially portable.** subside_group is forwarded; outtake type name is forwarded. The `error` exclusion flag is **not** in the webhook payload — the port must approximate `oo.error = 0 OR NULL` (e.g. treat all known types as non-error) or drop the filter. The DCD grouping hard-codes French type names (DCD, Euthanasier, Mort à l'arrivée…, Relacher, Transferer, Adoption) which are per-center vocabulary — identical names across instances cannot be guaranteed. |
| `Annexe_2B_2024` | `species_subside_group`, year | **Portable** with the same `error`-flag caveat. |
| `Annexe_2024` | `species_class`/`species_order` (both consolidated ✓) | **Portable** with the same `error`-flag caveat. |

### Missing, NOT portable today (data not in webhook payload) — reasons

| Creaves export | Why it cannot exist in the console today |
|---|---|
| `treatments_day_year` (treatments per day) | Treatments are **not part of the webhook contract** — no treatment events/fields are sent, the console DB has no treatment data at all. Would require a webhook contract extension (new payload section + console storage). |
| `animal_gavage` (animals currently force-fed) | Needs live care state (`force_feed`, `feeding`, current cage/zone occupancy). Only lifecycle snapshots are forwarded; current-care operational state is not. This is an operational day-to-day report, not a consolidated statistic. Would require a contract extension. |
| `controle_espèce` (animals with unknown species name) | Creaves checks `animals.species NOT IN (species reference table)`. The console has **no species reference table** — it only stores denormalized enrichment fields on each consolidated animal (null `species_class`/`agw_group` when the source species was unknown). An approximation ("species with no enrichment") is possible but flags the union of all centers' unknown species with subtly different semantics. |

### Creaves exports without console counterpart (by design)

- `/animals/search/export.csv` — exports the result of an interactive search;
  the console equivalent is the filterable Consolidated Animals view + CSV.

## Notes on Creaves query quality (observed during investigation)

- Most Creaves export WHERE clauses rely on MySQL operator precedence:
  `WHERE s.subside_group IN (...) AND oo.error = 0 OR oo.error IS NULL` — the
  `OR` binds loosely, so the subside-group restriction is effectively bypassed
  for rows with NULL outtaketype. Ports to the console should use explicit
  parentheses rather than replicating the precedence bug.

## Proposal (pending user decision)

1. Port the 3 trivial date aggregates (day / weekday / month).
2. Port the 3 Annexes with documented approximation for the `error` flag
   (and parenthesized WHERE clauses).
3. Document `treatments_day_year`, `animal_gavage`, `controle_espèce` as
   not portable without webhook contract extension (this file serves as the
   documented reason).
