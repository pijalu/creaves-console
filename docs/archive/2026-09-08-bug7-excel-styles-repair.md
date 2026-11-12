# Bug 7 — Excel export triggers Excel repair prompt: fix plan

Date: 2026-09-08
Scope: `creaves/excel` + `creaves-console/excel` (identical pipeline, fix applied to both).

## Symptom

`GET /export/excel?query=stat_communes|registre_detail` produces an .xlsx that
Microsoft Excel (macOS) flags for repair:

- `Removed Part: /xl/styles.xml part with XML error. (Styles) HRESULT 0x8000ffff`
- Cascade "repaired records" on worksheets + pivot tables (consequence of the
  styles removal, not independent corruption).

## Root cause (bisected 2026-09-08 with user)

| Test file | Content | Excel result |
|-----------|---------|--------------|
| TEST0 | raw embedded template | **clean** |
| TEST1 | template → excelize v2.8.0 open+save (no data, no pivot patch) | **styles.xml removed** |
| TEST2 | TEST1 + pivot-cache patch | broken (same log) |
| TEST3 | full served export | broken (same log) |

=> Corruption is introduced by **excelize's re-serialization of `styles.xml`**
on `WriteTo`/`SaveAs` (v2.8.0 AND v2.9.1 — same behavior observed). The
rewritten root element replaces the template's namespace declarations with
excelize's generic `templateNamespaceIDMap` (incl. duplicate `x15` token in
`mc:Ignorable`, 4.7 KB root element) and rewrites many style constructs
(`<b/>`→`<b val="1"/>`, `applyFont="1"`→`"true"`, tableStyle `table=`→`pivot=`,
dropped `xr9:uid`, added `<indexedColors/>`, …). Excel rejects the part
catastrophically; LibreOffice/openpyxl accept it. Our pivot-cache patch and
data writing are NOT the cause.

## Fix

In `writeExcelResponse` (both projects), after `f.WriteTo`, patch the final
zip archive: **replace `xl/styles.xml` with the template's original
styles.xml**, appending only the extra `<xf>` entries excelize legitimately
added while writing cells (observed: exactly one — the numFmtId=22 date style,
index 49, referenced by written date cells):

1. Parse template styles.xml from the embedded template (byte-exact,
   known-good — Excel opens TEST0 clean).
2. Parse the serialized styles.xml; extract `cellXfs` entries beyond the
   template count; sanitize each extra xf (drop empty `<alignment/>` child,
   drop `apply*` attributes that are false, normalize booleans to `1`).
3. If an extra xf references a custom numFmt (numFmtId ≥ 164): reuse the
   template numFmt with the same formatCode, else append a new numFmt with a
   fresh id (max template id + 1) and remap the xf.
4. Safety fallback: if the serialized styles.xml added fonts/fills/borders
   beyond the template counts (would make merged xfs reference out-of-range
   indices), keep the excelize styles.xml and log a warning instead of
   producing an invalid file.
5. Store merged styles.xml into the final archive (same zip-surgery helper as
   `truncateSheetRowsInZip`).

## Validation

- Candidates built by hand and validated by the user in Excel:
  - `C1_test1_tplstyles.xlsx` = TEST1 + template styles.xml verbatim
  - `C2_test3_mergedstyles.xlsx` = TEST3 + template styles.xml + appended xf
  Both must open with zero repair prompts. (pending user confirmation)
- Unit tests (`excel_test.go` / `pivot_test.go`, sqlite tag in console):
  - generated export's styles.xml root element == template root element;
  - `cellXfs count` == number of `<xf>` children == template count + extras;
  - every `s=` style index used in worksheets < cellXfs count;
  - no duplicate tokens in `mc:Ignorable`;
  - zip part round-trip keeps all other parts byte-identical.
- Regression: existing excel/pivot tests stay green (both projects).
- e2e (agent-browser, both apps): download
  `/export/excel?query=stat_communes` and `registre_detail`, assert the served
  styles.xml root == template root and zip is well-formed; final Excel open
  validation by user (guideline 5 evidence: commands + captured output).

## Code quality (guideline 7)

Run per project, each tool separately: `go vet ./...`, `staticcheck ./...`,
`gocognit -over 15 .`, `gocyclo -over 12 .`,
`go test -count=1 -race -cover ./...` (console: `CGO_ENABLED=1 -tags sqlite`).

## Commits

- `creaves`: `fix(export): restore template styles.xml in Excel exports — stop Excel repair prompt (Bug 7)`
- `creaves-console`: same message (packages kept in sync per package comment).

## Archive

After validation: move this plan + bug entry to `docs/archive/` (console) and
clear `bugs.md`.
