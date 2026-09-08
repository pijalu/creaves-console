//go:build sqlite
// +build sqlite

package actions

import (
	"net/http"
	"net/http/httptest"
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

// TestExportQueries_Registry guards the ported report registry: 28 stable
// ids, unique, and findExportQuery is case-insensitive.
func TestExportQueries_Registry(t *testing.T) {
	require.Len(t, exportQueries, 28, "expected 28 ported export queries")

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

	// bugs.md datatable item: sort/filter/pagination delegated to DataTables;
	// the hand-rolled sort/filter JS must be gone.
	assert.Contains(t, body, `id="exportTable"`)
	assert.Contains(t, body, `$('#exportTable').DataTable({`)
	assert.Contains(t, body, "deferRender: true")
	assert.NotContains(t, body, "sortExportTable")
	assert.NotContains(t, body, "filterExportTable")

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

// TestExportReports_DateAggregates covers the item-4 date aggregate reports
// ({year:...}, {dow:...}, {df:...} placeholders on intake_date): 5 seeded
// animals, intakes 2024-01-10..12 shared across centers (Wed..Fri).
func TestExportReports_DateAggregates(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)
	app := newExportReportsTestApp(tx, true)

	// Per day of year: 3 distinct intake days (Jan 10/11/12, shared across
	// centers), counts 2/2/1; both dialect paths exercised ({year:...} ->
	// strftime('%Y', ...), {df:...%Y %m %d}).
	rec := getExport(t, app, "/export/reports/export.csv?query=entry_date_year")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body := strings.TrimPrefix(rec.Body.String(), "\ufeff")
	lines := strings.Split(strings.TrimSpace(body), "\n")
	// 1 header + 3 distinct intake days.
	assert.Len(t, lines, 4)
	assert.Contains(t, lines[0], "Date d'Entrée")
	assert.Contains(t, body, "2024;2024 01 10;2")
	assert.Contains(t, body, "2024;2024 01 12;1")

	// Per weekday: 3 distinct weekdays; 2024-01-12 is a Friday -> 6.
	rec = getExport(t, app, "/export/reports/export.csv?query=entry_day_week")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body = strings.TrimPrefix(rec.Body.String(), "\ufeff")
	lines = strings.Split(strings.TrimSpace(body), "\n")
	assert.Len(t, lines, 4)
	assert.Contains(t, lines[0], "1=dimanche")
	assert.Contains(t, body, "2024;4;2", "Wednesday 2024-01-10 must map to DAYOFWEEK 4")
	assert.Contains(t, body, "2024;6;1", "Friday 2024-01-12 must map to DAYOFWEEK 6")

	// Per month: single January row with 5 animals.
	rec = getExport(t, app, "/export/reports/export.csv?query=day_to_month")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body = strings.TrimPrefix(rec.Body.String(), "\ufeff")
	lines = strings.Split(strings.TrimSpace(body), "\n")
	assert.Len(t, lines, 2)
	assert.Contains(t, lines[1], "2024;01;5")

	// Scope filter: center-b has 2 animals on 2024-01-10/11.
	rec = getExport(t, app, "/export/reports/export.csv?query=entry_date_year&instance_id=center-b")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body = strings.TrimPrefix(rec.Body.String(), "\ufeff")
	lines = strings.Split(strings.TrimSpace(body), "\n")
	assert.Len(t, lines, 3, "scoped report must only contain center-b days")
	assert.NotContains(t, body, "2024 01 12", "2024-01-12 has only center-a animals")
}

// seedAnnexeAnimal creates one consolidated animal with the fields the
// Annexe reports group/filter on.
func seedAnnexeAnimal(t *testing.T, tx *pop.Connection, instanceID string, animalID, year, yearNumber int, species string, subsideGroup, class, order, outtakeType *string) {
	t.Helper()
	seedAnnexeAnimalErr(t, tx, instanceID, animalID, year, yearNumber, species, subsideGroup, class, order, outtakeType, nil)
}

func seedAnnexeAnimalErr(t *testing.T, tx *pop.Connection, instanceID string, animalID, year, yearNumber int, species string, subsideGroup, class, order, outtakeType *string, outtakeError *bool) {
	t.Helper()
	now := time.Now().UTC()
	a := &models.ConsolidatedAnimal{
		ID:            uuid.Must(uuid.NewV4()),
		InstanceID:    instanceID,
		AnimalID:      animalID,
		Year:          year,
		YearNumber:    yearNumber,
		Species:       nulls.NewString(species),
		IntakeDate:    nulls.NewTime(time.Date(year, 1, 10, 0, 0, 0, 0, time.UTC)),
		OuttakeDate:   nulls.NewTime(time.Date(year, 2, 10, 0, 0, 0, 0, time.UTC)),
		CurrentStatus: "released",
		LastEventAt:   now,
	}
	if subsideGroup != nil {
		a.SpeciesSubsideGroup = nulls.NewString(*subsideGroup)
	}
	if class != nil {
		a.SpeciesClass = nulls.NewString(*class)
	}
	if order != nil {
		a.SpeciesOrder = nulls.NewString(*order)
	}
	if outtakeType != nil {
		a.OuttakeType = nulls.NewString(*outtakeType)
	}
	if outtakeError != nil {
		a.OuttakeError = nulls.NewBool(*outtakeError)
	}
	require.NoError(t, tx.Create(a))
}

func strptr(s string) *string { return &s }
func boolptr(b bool) *bool    { return &b }

// TestExportReports_AnnexeReports covers the 3 item-4 Annexe reports against
// a fixture spanning all subside groups and class/order branches, incl. an
// unknown species (no enrichment) and a non-SG animal.
func TestExportReports_AnnexeReports(t *testing.T) {
	tx := setupTest(t)
	seedExcelInstances(t, tx)

	// Fixture (center-a unless noted):
	//  id 10  Buse        SG2 + Aves            outtake "Relacher"
	//  id 11  Hibou       SG1 + Aves            outtake "DCD"
	//  id 12  Hérisson    SG3 + Mammalia/Eulipotyphla  outtake "Mort à l'arrivée avant l'encodage" (-> DCD)
	//  id 13  Chauve-souris (no SG) + Chiroptera  (center-b)
	//  id 14  Inconnue    no enrichment at all  (unknown species)
	seedAnnexeAnimal(t, tx, "center-a", 10, 2024, 1, "Buse variable", strptr("SG2"), strptr("Aves"), nil, strptr("Relacher"))
	seedAnnexeAnimal(t, tx, "center-a", 11, 2024, 2, "Hibou moyen-duc", strptr("SG1"), strptr("Aves"), nil, strptr("DCD"))
	seedAnnexeAnimal(t, tx, "center-a", 12, 2024, 3, "Hérisson", strptr("SG3"), strptr("Mammalia"), strptr("Eulipotyphla"), strptr("Mort à l'arrivée avant l'encodage"))
	seedAnnexeAnimal(t, tx, "center-b", 13, 2024, 1, "Pipistrelle", nil, strptr("Mammalia"), strptr("Chiroptera"), strptr("Transferer"))
	seedAnnexeAnimal(t, tx, "center-a", 14, 2024, 4, "Espèce inconnue", nil, nil, nil, nil)
	// id 15: error-flagged outtake type — must be excluded from all 3 Annexe
	// reports (the webhook now forwards outtaketypes.error as outtake_error).
	seedAnnexeAnimalErr(t, tx, "center-a", 15, 2024, 5, "Martre", strptr("SG3"), strptr("Mammalia"), strptr("Carnivora"), strptr("Relacher"), boolptr(true))
	// id 16: explicit error=false outtake — must be kept (real non-error).
	seedAnnexeAnimalErr(t, tx, "center-a", 16, 2024, 6, "Fouine", strptr("SG3"), strptr("Mammalia"), strptr("Carnivora"), strptr("Relacher"), boolptr(false))
	app := newExportReportsTestApp(tx, true)

	// Annexe_2A_2024: detail rows for SG1/SG2/SG3 animals without the error
	// flag (4 of 7), group labels + outtake-type mapping.
	rec := getExport(t, app, "/export/reports/export.csv?query=Annexe_2A_2024")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body := strings.TrimPrefix(rec.Body.String(), "\ufeff")
	lines := strings.Split(strings.TrimSpace(body), "\n")
	// 1 header + 4 kept SG animals (Pipistrelle/unknown excluded: non-SG;
	// Martre excluded: outtake_error=1; Fouine kept: outtake_error=0).
	assert.Len(t, lines, 5)
	assert.Contains(t, lines[0], "Groupe")
	assert.Contains(t, body, "A) Mammifères non volants")
	assert.Contains(t, body, "B) Rapaces, oiseaux d’eau, échassiers ou limicoles")
	assert.Contains(t, body, "C) Autres oiseaux et chauves-souris, batraciens et reptiles")
	assert.Contains(t, body, "DCD")
	assert.Contains(t, body, "Relacher")
	assert.Contains(t, body, "Fouine", "error=false outtake must be kept")
	assert.NotContains(t, body, "Martre", "error-flagged outtake must be excluded")
	assert.NotContains(t, body, "Pipistrelle", "non-SG animal must be excluded")
	assert.NotContains(t, body, "inconnue", "unknown species must be excluded")

	// Scoped to center-b: no SG animals there -> header only.
	rec = getExport(t, app, "/export/reports/export.csv?query=Annexe_2A_2024&instance_id=center-b")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	lines = strings.Split(strings.TrimSpace(strings.TrimPrefix(rec.Body.String(), "\ufeff")), "\n")
	assert.Len(t, lines, 1, "center-b has no SG animals")

	// Annexe_2B_2024: counts per year x subside group -> 3 rows; the SG3 row
	// counts 2 (Hérisson + Fouine), error-flagged Martre excluded.
	rec = getExport(t, app, "/export/reports/export.csv?query=Annexe_2B_2024")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body = strings.TrimPrefix(rec.Body.String(), "\ufeff")
	lines = strings.Split(strings.TrimSpace(body), "\n")
	assert.Len(t, lines, 4)
	assert.Contains(t, lines[0], "Groupes SUBSIDE")
	assert.Contains(t, body, "(50/tranche)")
	assert.Contains(t, body, "(100/tranche);2", "SG3 counts Hérisson + Fouine, excludes error-flagged Martre")

	// Annexe_2024: Oiseaux = 2 (both Aves), Mammifères non volants = 2
	// (Eulipotyphla + Carnivora Fouine; Martre excluded), Mammifères volants
	// et autres = 1 (Chiroptera); remaining animals fall into an empty group
	// row.
	rec = getExport(t, app, "/export/reports/export.csv?query=Annexe_2024")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body = strings.TrimPrefix(rec.Body.String(), "\ufeff")
	assert.Contains(t, body, "Oiseaux;2")
	assert.Contains(t, body, "Mammifères non volants;2")
	assert.Contains(t, body, "Mammifères volants et autres espèces;1")

	// Scoped Annexe_2024 to center-b: only the Chiroptera row remains.
	rec = getExport(t, app, "/export/reports/export.csv?query=Annexe_2024&instance_id=center-b")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	body = strings.TrimPrefix(rec.Body.String(), "\ufeff")
	assert.NotContains(t, body, "Oiseaux;", "scoped report must exclude center-a birds")
	assert.Contains(t, body, "Mammifères volants et autres espèces;1")

	// Online view of one Annexe report renders.
	rec = getExport(t, app, "/export/reports/view?query=Annexe_2B_2024")
	require.Equal(t, http.StatusOK, rec.Code, "body: %.300s", rec.Body.Bytes())
	assert.Contains(t, rec.Body.String(), "3 row(s)")
}
