//go:build sqlite
// +build sqlite

package actions

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"creaves-console/models"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newExcelTestApp mounts the excel export route with the shared sqlite test
// DB and a signed-in user, mimicking popmw + SetCurrentUser.
func newExcelTestApp(tx *pop.Connection, loggedIn bool) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			if loggedIn {
				c.Set("current_user", &models.User{ID: uuid.Must(uuid.NewV4()), Login: "reporter", Admin: false})
			}
			return next(c)
		}
	})
	if !loggedIn {
		// Mimic the production Authorize middleware: redirect anonymous users.
		app.Use(Authorize)
	}
	app.GET("/export/excel", ExportExcel)
	return app
}

// seedExcelInstances registers two instances and seeds consolidated animals:
// 3 rows for center-a, 2 rows for center-b.
func seedExcelInstances(t *testing.T, tx *pop.Connection) {
	t.Helper()
	require.NoError(t, tx.RawQuery("DELETE FROM consolidated_animals").Exec())
	require.NoError(t, tx.RawQuery("DELETE FROM creaves_instances").Exec())

	now := time.Now().UTC()
	for _, id := range []string{"center-a", "center-b"} {
		require.NoError(t, tx.Create(&models.CreavesInstance{
			ID: uuid.Must(uuid.NewV4()), InstanceID: id, Name: id,
			FirstSeenAt: now, LastSeenAt: now,
		}))
	}
	seed := func(instanceID string, n int) {
		for i := 0; i < n; i++ {
			a := &models.ConsolidatedAnimal{
				ID:         uuid.Must(uuid.NewV4()),
				InstanceID: instanceID,
				AnimalID:   i + 1,
				Year:       2024, YearNumber: i + 1,
				Species:       nulls.NewString("Hérisson"),
				AnimalAge:     nulls.NewString("Adulte"),
				Gender:        nulls.NewString("Mâle"),
				IntakeDate:    nulls.NewTime(time.Date(2024, 1, 10+i, 0, 0, 0, 0, time.UTC)),
				OuttakeDate:   nulls.NewTime(time.Date(2024, 2, 10+i, 0, 0, 0, 0, time.UTC)),
				OuttakeType:   nulls.NewString("Relâcher"),
				EntryCause:    nulls.NewString("Blessé"),
				DiscoveryCity: nulls.NewString("Vielsalm"),
				CurrentStatus: "released",
				LastEventAt:   now,
			}
			require.NoError(t, tx.Create(a))
		}
	}
	seed("center-a", 3)
	seed("center-b", 2)
}

// xlsxParts unzips an XLSX response body into a part-name -> content map.
func xlsxParts(t *testing.T, body []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)
	parts := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		b, err := io.ReadAll(rc)
		rc.Close()
		require.NoError(t, err)
		parts[f.Name] = b
	}
	return parts
}

var (
	pivotRefRe     = regexp.MustCompile(`<worksheetSource[^>]*ref="([^"]+)"`)
	pivotSheetRe   = regexp.MustCompile(`<worksheetSource[^>]*sheet="([^"]+)"`)
	recordCountRe  = regexp.MustCompile(`recordCount="(\d+)"`)
	refreshOnLoad  = regexp.MustCompile(`refreshOnLoad="1"`)
	recordsCountRe = regexp.MustCompile(`<pivotCacheRecords[^>]*count="(\d+)"`)
	sheetRowNumsRe = regexp.MustCompile(`<row[^>]*\br="(\d+)"`)
	filterDBRe     = regexp.MustCompile(`name="_xlnm\._FilterDatabase"[^>]*>([^<]+)`)
	dimensionRe    = regexp.MustCompile(`<dimension[^>]*ref="([^"]+)"`)
)

// workbookSheetPath resolves the xl/worksheets/sheetN.xml part path of a
// named sheet from the workbook.xml + rels parts of an XLSX archive.
func workbookSheetPath(t *testing.T, parts map[string][]byte, sheetName string) string {
	t.Helper()
	wb, ok := parts["xl/workbook.xml"]
	require.True(t, ok, "xl/workbook.xml missing")
	rels, ok := parts["xl/_rels/workbook.xml.rels"]
	require.True(t, ok, "workbook rels missing")

	sheetRe := regexp.MustCompile(`<sheet[^>]*name="` + regexp.QuoteMeta(sheetName) + `"[^>]*r:id="([^"]+)"`)
	m := sheetRe.FindSubmatch(wb)
	require.NotNil(t, m, "sheet %q not found in workbook.xml", sheetName)

	relRe := regexp.MustCompile(`<Relationship[^>]*Id="` + string(m[1]) + `"[^>]*Target="([^"]+)"`)
	m = relRe.FindSubmatch(rels)
	require.NotNil(t, m, "relationship for sheet %q not found", sheetName)
	target := string(m[1])
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	return "xl/" + target
}

// assertPivotCache verifies the Bug 5 fixups on a generated workbook: the
// pivot cache points at the real written range, recordCount matches the data
// rows, refreshOnLoad is set, cached records are emptied and the data sheet
// holds exactly header+recordCount rows.
func assertPivotCache(t *testing.T, parts map[string][]byte, sheet, lastCol string, dataRows int) {
	t.Helper()

	def, ok := parts["xl/pivotCache/pivotCacheDefinition1.xml"]
	require.True(t, ok, "pivotCacheDefinition1.xml missing")
	expectedRef := fmt.Sprintf("A1:%s%d", lastCol, dataRows+1)

	m := pivotRefRe.FindSubmatch(def)
	require.NotNil(t, m, "worksheetSource ref missing")
	assert.Equal(t, expectedRef, string(m[1]))

	sm := pivotSheetRe.FindSubmatch(def)
	require.NotNil(t, sm, "worksheetSource sheet missing")
	assert.Equal(t, sheet, string(sm[1]))

	cm := recordCountRe.FindSubmatch(def)
	require.NotNil(t, cm, "recordCount missing")
	assert.Equal(t, strconv.Itoa(dataRows), string(cm[1]))

	assert.True(t, refreshOnLoad.Match(def), "refreshOnLoad=1 missing")

	// Cached records must be emptied so Excel rebuilds from the source range.
	rec, ok := parts["xl/pivotCache/pivotCacheRecords1.xml"]
	require.True(t, ok, "pivotCacheRecords1.xml missing")
	rm := recordsCountRe.FindSubmatch(rec)
	require.NotNil(t, rm, "pivotCacheRecords count missing")
	assert.Equal(t, "0", string(rm[1]))

	// The data sheet must hold exactly the written rows (no leftover
	// template rows) and the dimension must cover exactly the written range.
	sheetXML, ok := parts[workbookSheetPath(t, parts, sheet)]
	require.True(t, ok, "data sheet part for %q missing", sheet)
	rows := sheetRowNumsRe.FindAllSubmatch(sheetXML, -1)
	assert.Len(t, rows, dataRows+1, "data sheet row count")
	dm := dimensionRe.FindSubmatch(sheetXML)
	require.NotNil(t, dm, "dimension missing")
	assert.Equal(t, expectedRef, string(dm[1]))
}

// downloadExcel performs GET /export/excel and returns the recorded response.
func downloadExcel(t *testing.T, app *buffalo.App, url string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func TestExportExcel_UnknownQuery404(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExcelTestApp(tx, true)

	rec := downloadExcel(t, app, "/export/excel?query=nope")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestExportExcel_UnknownInstance404(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExcelTestApp(tx, true)

	rec := downloadExcel(t, app, "/export/excel?query=registre_detail&instance_id=center-missing")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestExportExcel_AnonymousRedirected(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExcelTestApp(tx, false)

	rec := downloadExcel(t, app, "/export/excel?query=registre_detail")
	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/auth/new", rec.Header().Get("Location"))
}

func TestExportExcel_RegistreDetail_Global(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExcelTestApp(tx, true)

	rec := downloadExcel(t, app, "/export/excel?query=registre_detail")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.200s", rec.Body.Bytes())
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `filename="registre_detail.xlsx"`)

	parts := xlsxParts(t, rec.Body.Bytes())
	// Global scope: 3 + 2 = 5 data rows, 33 columns (A..AG).
	assertPivotCache(t, parts, "animals", "AG", 5)
	// The registre template ships no _xlnm._FilterDatabase defined name
	// (unlike stats_communes), so nothing to assert here beyond the cache.
}

func TestExportExcel_RegistreDetail_InstanceScoped(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExcelTestApp(tx, true)

	rec := downloadExcel(t, app, "/export/excel?query=registre_detail&instance_id=center-b")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.200s", rec.Body.Bytes())
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `filename="registre_detail_center-b.xlsx"`)

	parts := xlsxParts(t, rec.Body.Bytes())
	// Instance scope: center-b has 2 data rows.
	assertPivotCache(t, parts, "animals", "AG", 2)
}

func TestExportExcel_StatCommunes_GlobalAndScoped(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExcelTestApp(tx, true)

	rec := downloadExcel(t, app, "/export/excel?query=stat_communes")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.200s", rec.Body.Bytes())
	parts := xlsxParts(t, rec.Body.Bytes())
	// 14 columns (A..N), 5 data rows globally.
	assertPivotCache(t, parts, "bdd", "N", 5)

	// _xlnm._FilterDatabase must cover the written range.
	fm := filterDBRe.FindSubmatch(parts["xl/workbook.xml"])
	require.NotNil(t, fm, "_FilterDatabase defined name missing")
	assert.Contains(t, string(fm[1]), "bdd!$A$1:$N$6")

	rec = downloadExcel(t, app, "/export/excel?query=stat_communes&instance_id=center-a")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.200s", rec.Body.Bytes())
	parts = xlsxParts(t, rec.Body.Bytes())
	// center-a has 3 data rows.
	assertPivotCache(t, parts, "bdd", "N", 3)
}

func TestReportsCSVIndex_RendersExcelLinks(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)

	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			c.Set("current_user", &models.User{ID: uuid.Must(uuid.NewV4()), Login: "reporter"})
			return next(c)
		}
	})
	app.GET("/reports/csv", ReportsCSVIndex)

	req := httptest.NewRequest(http.MethodGet, "/reports/csv?instance_id=center-a", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body := rec.Body.String()
	assert.Contains(t, body, `/export/excel?query=registre_detail&amp;instance_id=center-a`)
	assert.Contains(t, body, `/export/excel?query=stat_communes&amp;instance_id=center-a`)
}
