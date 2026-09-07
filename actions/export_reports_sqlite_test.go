//go:build sqlite
// +build sqlite

package actions

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"creaves-console/models"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExportQueries_Registry guards the ported report registry: 22 stable
// ids, unique, and findExportQuery is case-insensitive.
func TestExportQueries_Registry(t *testing.T) {
	require.Len(t, exportQueries, 22, "expected 22 ported export queries")

	seen := map[string]bool{}
	for _, q := range exportQueries {
		assert.NotEmpty(t, q.Name)
		assert.NotEmpty(t, q.Description)
		assert.NotEmpty(t, q.SQL)
		lc := strings.ToLower(q.Name)
		assert.False(t, seen[lc], "duplicate query id %q", q.Name)
		seen[lc] = true
	}

	assert.NotNil(t, findExportQuery("register"))
	assert.NotNil(t, findExportQuery("Register"), "lookup must be case-insensitive")
	assert.NotNil(t, findExportQuery("AGW_group"), "original Creaves casing must resolve")
	assert.NotNil(t, findExportQuery("agw_group"))
	assert.Nil(t, findExportQuery("nope"))
	assert.Nil(t, findExportQuery(""))
}

// newExportReportsTestApp mounts the export report routes with the shared
// sqlite test DB and a signed-in user, mimicking popmw + SetCurrentUser.
func newExportReportsTestApp(tx *pop.Connection, loggedIn bool) *buffalo.App {
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
		app.Use(Authorize)
	}
	app.GET("/export/reports", ExportReportsIndex)
	app.GET("/export/reports/view", ExportReportView)
	app.GET("/export/reports/export.csv", ExportReportCSV)
	return app
}

func getExport(t *testing.T, app *buffalo.App, url string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func TestExportReports_IndexRendersAllQueries(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExportReportsTestApp(tx, true)

	rec := getExport(t, app, "/export/reports?instance_id=center-a")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body := rec.Body.String()
	for _, q := range exportQueries {
		assert.Contains(t, body, "/export/reports/view?query="+q.Name+"&amp;instance_id=center-a", "missing view link for %q", q.Name)
		assert.Contains(t, body, "/export/reports/export.csv?query="+q.Name+"&amp;instance_id=center-a", "missing CSV link for %q", q.Name)
	}
}

func TestExportReports_UnknownQuery404(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExportReportsTestApp(tx, true)

	rec := getExport(t, app, "/export/reports/view?query=nope")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = getExport(t, app, "/export/reports/export.csv?query=nope")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestExportReports_UnknownInstance404(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExportReportsTestApp(tx, true)

	rec := getExport(t, app, "/export/reports/view?query=register&instance_id=center-missing")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestExportReports_AnonymousRedirected(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExportReportsTestApp(tx, false)

	rec := getExport(t, app, "/export/reports")
	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/auth/new", rec.Header().Get("Location"))
}

// TestExportReports_RegisterView_GlobalAndScoped runs the "register" report
// (uses {df:...} and {scopeWhere} placeholders) through the real sqlite
// engine: global scope lists all 5 seeded animals, instance scope only the
// instance's rows.
func TestExportReports_RegisterView_GlobalAndScoped(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExportReportsTestApp(tx, true)

	// Global: 5 animals total (3 center-a + 2 center-b).
	rec := getExport(t, app, "/export/reports/view?query=register")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body := rec.Body.String()
	assert.Contains(t, body, "5 row(s)")
	assert.Contains(t, body, "center-a")
	assert.Contains(t, body, "center-b")
	// Dates must be rendered dd/mm/yyyy by the sqlite strftime path.
	assert.Contains(t, body, "10/01/2024")

	// Scoped to center-b: 2 animals.
	rec = getExport(t, app, "/export/reports/view?query=register&instance_id=center-b")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body = rec.Body.String()
	assert.Contains(t, body, "2 row(s)")
	assert.Contains(t, body, "center-b")
	assert.NotContains(t, body, ">center-a<", "instance scope must filter out other centers")
}

// TestExportReports_CSV checks the CSV download path: header row, BOM,
// instance column and per-instance row counts.
func TestExportReports_CSV(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExportReportsTestApp(tx, true)

	rec := getExport(t, app, "/export/reports/export.csv?query=register")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/csv")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `filename="register.csv"`)

	body := rec.Body.String()
	assert.True(t, strings.HasPrefix(body, "\ufeff"), "CSV must start with the UTF-8 BOM")
	lines := strings.Split(strings.TrimPrefix(strings.TrimSpace(body), "\ufeff"), "\n")
	// 1 header + 5 data rows.
	assert.Len(t, lines, 6)
	assert.Contains(t, lines[0], "Instance")

	// Scoped: 1 header + 2 rows, instance-filtered filename.
	rec = getExport(t, app, "/export/reports/export.csv?query=register&instance_id=center-a")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	assert.Contains(t, rec.Header().Get("Content-Disposition"), `filename="register-center-a.csv"`)
	lines = strings.Split(strings.TrimPrefix(strings.TrimSpace(rec.Body.String()), "\ufeff"), "\n")
	assert.Len(t, lines, 4)
}

// TestExportReports_AllQueriesRunOnSQLite executes every registered query
// against the seeded sqlite DB to guarantee the dialect placeholders
// ({df:...}, {stay}, {true}, {scopeWhere}/{scopeAnd}) translate to valid
// SQLite SQL.
func TestExportReports_AllQueriesRunOnSQLite(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExportReportsTestApp(tx, true)

	for _, q := range exportQueries {
		rec := getExport(t, app, "/export/reports/export.csv?query="+q.Name)
		require.Equal(t, http.StatusOK, rec.Code, "query %q failed: %.300s", q.Name, rec.Body.Bytes())
		if !q.Aggregate {
			// Row-level reports must expose the Instance column as last header.
			first := strings.SplitN(strings.TrimPrefix(rec.Body.String(), "\ufeff"), "\n", 2)[0]
			assert.True(t, strings.HasSuffix(strings.TrimSpace(first), "Instance"), "query %q must end header with Instance, got %q", q.Name, first)
		}

		// And with an instance scope.
		rec = getExport(t, app, "/export/reports/export.csv?query="+q.Name+"&instance_id=center-a")
		require.Equal(t, http.StatusOK, rec.Code, "scoped query %q failed: %.300s", q.Name, rec.Body.Bytes())
	}
}
