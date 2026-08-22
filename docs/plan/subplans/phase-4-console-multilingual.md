# Phase 4 — Console multilingual UI & localized reference data

Parent: [`../SYNC_IMPLEMENTATION_PLAN.md`](../SYNC_IMPLEMENTATION_PLAN.md) · Spec: [`../../specs/SYNC_SPEC.md`](../../specs/SYNC_SPEC.md) (§5)
Findings fixed: **F5** (fr missing), **F4** (no localized reference display). Requirements: **R6**, **R5 display side**.
Depends on: Phase 2 T2.3/T2.4 (`translations` column on `consolidated_animals`). Templates created in Phase 2 (`instances/*`) get real fr translations here.

## T4.1 French locale scaffolding

- Files: `locales/all.fr.yaml`, `templates/**/*.plush.fr.html` (copy of base for every template), `actions/render.go` uiLanguages + `switchLanguage.go` allowlist if any.
- **RED** `TestLangSwitch_FrenchOfferedAndSwitchable`: `GET /lang/?lang=fr&url=/dashboard` sets cookie `lang=fr`; `GET /dashboard` renders the `dashboard/index.plush.fr.html` variant (assert a French-only marker string present in that template, e.g. its `<html lang="fr">`).
- **RED** `TestRenderFr_VariantsExistForAllTemplates`: walk `templates.FS()`; for every `*.plush.html` (and de/nl) assert a `.plush.fr.html` sibling exists (guard against future templates missing fr).
- **GREEN**: copy templates, translate the strings via `locales/all.fr.yaml` (`t` helper keys), wire uiLanguages `{"fr", "Français"}`.

## T4.2 Localized reference display (spec §5)

- Files: `actions/render.go` (helper), `models/consolidated_animal.go` (method), templates where species/type/age/zone/outtake/entry_cause render; tests `models/consolidated_animal_test.go` + a template-level assertion via rendered HTML string in `actions/dashboard_test.go`.
- **RED** `TestConsolidatedAnimal_LocalizedField`: animal with `species=Hérisson` + translations `{"en-US":{"species":"Hedgehog"},"de":{...}}` → `LocalizedField("en-US","species")=="Hedgehog"`, `("de","species")` correct, `("nl","species")=="Hérisson"` (fallback), `("fr","species")=="Hérisson"`.
- **RED** `TestConsolidatedAnimalsIndex_RendersLocalizedSpecies`: request index with `lang=fr` cookie vs `lang=en-US` cookie → same row data, different species label strings in HTML.
- **RED** `TestReportsByType_LocalizedGroupLabels`: two animals of canonical type `Mammifère`, translations include `en-US` "Mammal"; `lang=en-US` → table shows "Mammal" while grouping still merges both rows into one (count 2).
- **GREEN**: `LocalizedField(lang, field)` resolves from stored `translations` JSON w/ canonical fallback; plush helper wraps it; report label lookup = first-seen canonical→localized map from the result set (grouping keys stay canonical — spec §3.4).
