//go:build sqlite
// +build sqlite

package actions

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
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

// newRegisterTestApp builds a minimal Buffalo app injecting the shared
// testDB and mounting the register + snapshot routes.
func newRegisterTestApp(tx *pop.Connection) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/reports/register", RegisterIndex)
	app.GET("/reports/register/export.csv", RegisterExportCSV)
	app.GET("/reports/snapshot", SnapshotIndex)
	app.GET("/reports/snapshot/export.csv", SnapshotExportCSV)
	return app
}

// seedRegisterFixtures resets the tables and inserts known animals:
//   - center-a 2024: 3 animals (numbers 1,2,3), one with outtake on
//     2024/03/01, one with outtake on 2024/01/05 (removed early)
//   - center-b 2024: 1 animal
//   - center-a 2023: 1 animal, outtaken 2023/06/01 — proves year filtering
//     for the register and keeps it out of the snapshot dates under test
//
// Intake dates: 2024 animals on 2024/01/02..04, 2023 animal on 2023/05/10.
func seedRegisterDateFixtures(t *testing.T, tx *pop.Connection) {
	t.Helper()
	require.NoError(t, tx.RawQuery("DELETE FROM consolidated_animals").Exec())
	require.NoError(t, tx.RawQuery("DELETE FROM creaves_instances").Exec())

	now := time.Now().UTC()
	for _, id := range []string{"center-a", "center-b"} {
		inst := &models.CreavesInstance{
			ID: uuid.Must(uuid.NewV4()), InstanceID: id, Name: "Name-" + id,
			FirstSeenAt: now, LastSeenAt: now,
		}
		require.NoError(t, tx.Create(inst))
	}

	type row struct {
		instance string
		animalID int
		year     int
		intake   time.Time
		outtake  *time.Time
	}
	mk := func(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC) }
	outtakeMarch := mk(2024, 3, 1)
	outtakeJan := mk(2024, 1, 5)
	outtake2023 := mk(2023, 6, 1)
	rows := []row{
		{instance: "center-a", animalID: 1, year: 2024, intake: mk(2024, 1, 2), outtake: &outtakeMarch}, // in care until 2024/03/01
		{instance: "center-a", animalID: 2, year: 2024, intake: mk(2024, 1, 3), outtake: &outtakeJan},   // in care until 2024/01/05
		{instance: "center-a", animalID: 3, year: 2024, intake: mk(2024, 1, 4)},                         // still in care
		{instance: "center-b", animalID: 1, year: 2024, intake: mk(2024, 1, 4)},                         // still in care
		{instance: "center-a", animalID: 9, year: 2023, intake: mk(2023, 5, 10), outtake: &outtake2023}, // previous year
	}
	for i, r := range rows {
		a := &models.ConsolidatedAnimal{
			ID:            uuid.Must(uuid.NewV4()),
			InstanceID:    r.instance,
			AnimalID:      r.animalID,
			Year:          r.year,
			YearNumber:    i + 1,
			CurrentStatus: "in_care",
			LastEventAt:   now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		a.Species = nulls.NewString("Hérisson")
		a.AnimalType = nulls.NewString("Mammifère")
		a.Ring = nulls.NewString("FR-" + r.instance + "-" + strconv.Itoa(r.animalID))
		a.AnimalAge = nulls.NewString("Adulte")
		a.EntryCause = nulls.NewString("Collision")
		a.DiscoveryLocation = nulls.NewString("Strasbourg")
		a.Zone = nulls.NewString("Quarantine")
		a.Cage = nulls.NewString("A12")
		a.IntakeDate = nulls.NewTime(r.intake)
		if r.outtake != nil {
			a.OuttakeDate = nulls.NewTime(*r.outtake)
			a.OuttakeType = nulls.NewString("Relâché")
		}
		require.NoError(t, tx.Create(a))
	}
}

func TestRegister_Index_Global_DefaultLatestYear(t *testing.T) {
	seedRegisterDateFixtures(t, testDB)
	app := newRegisterTestApp(testDB)

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/register", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	// Latest year (2024) is selected by default; 2023 rows are excluded.
	assert.Contains(t, body, "Total animals for 2024: <strong>4</strong>")
	assert.Contains(t, body, "center-a")
	assert.Contains(t, body, "center-b")
	assert.Contains(t, body, "Hérisson")
	// The 2023 animal (year number 5) must not appear.
	assert.NotContains(t, body, ">5</a>")
}

func TestRegister_Index_YearFilter(t *testing.T) {
	seedRegisterDateFixtures(t, testDB)
	app := newRegisterTestApp(testDB)

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/register?year=2023", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Total animals for 2023: <strong>1</strong>")
	assert.Contains(t, body, ">5</a>")
	// 2023 has only the center-a animal; the scoped Center column is hidden
	// and no center-b cell is rendered (dropdown options still list it).
	assert.NotContains(t, body, ">center-b<")
}

func TestRegister_Index_InstanceScope(t *testing.T) {
	seedRegisterDateFixtures(t, testDB)
	app := newRegisterTestApp(testDB)

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/register?year=2024&instance_id=center-b", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Total animals for 2024: <strong>1</strong>")
	assert.Contains(t, body, "center-b")
	assert.NotContains(t, body, ">center-a<")
}

func TestRegister_UnknownInstance404(t *testing.T) {
	seedRegisterDateFixtures(t, testDB)
	app := newRegisterTestApp(testDB)

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/register?year=2024&instance_id=center-missing", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRegister_EmptyDatabase(t *testing.T) {
	seedRegisterDateFixtures(t, testDB)
	require.NoError(t, testDB.RawQuery("DELETE FROM consolidated_animals").Exec())
	app := newRegisterTestApp(testDB)

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/register", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "No register year available yet.")
}

func TestRegister_ExportCSV(t *testing.T) {
	seedRegisterDateFixtures(t, testDB)
	app := newRegisterTestApp(testDB)

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/register/export.csv?year=2024", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "registertable-2024.csv")

	parsed := csv.NewReader(strings.NewReader(rec.Body.String()))
	parsed.Comma = ';'
	records, err := parsed.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 5) // header + 4 animals
	assert.Equal(t, []string{"\ufeffInstance", "Number", "Type", "Species", "Identification",
		"Entry Date", "Discovery Location", "Age", "Reason", "Outtake date",
		"Outtake reason", "Location"}, records[0])
	// 3 center-a + 1 center-b rows.
	expectedDates := map[string]bool{"02/01/2024": true, "03/01/2024": true, "04/01/2024": true}
	centers := map[string]int{}
	for _, r := range records[1:] {
		centers[r[0]]++
		assert.True(t, expectedDates[r[5]], "unexpected entry date %q", r[5])
	}
	assert.Equal(t, 3, centers["center-a"])
	assert.Equal(t, 1, centers["center-b"])

	// Scoped export: filename carries the instance.
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/register/export.csv?year=2024&instance_id=center-a", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "registertable-2024-center-a.csv")
	records, err = csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	require.NoError(t, err)
	assert.Len(t, records, 4) // header + 3 animals
}

func TestSnapshot_Index_Logic(t *testing.T) {
	seedRegisterDateFixtures(t, testDB)
	app := newRegisterTestApp(testDB)

	// On 2024/01/03: animals 1 (intake 01/02) and 2 (intake 01/03) are in
	// care; animal 3 intakes on 01/04 (absent); animal 2's outtake is on
	// 01/05 (still present on 01/03). 2023 animal excluded by intake date.
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/snapshot?snapshotDate=2024-01-03", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Animals in care on 2024-01-03: <strong>2</strong>")
	assert.Contains(t, body, ">1</a>")
	assert.Contains(t, body, ">2</a>")
	assert.NotContains(t, body, ">3</a>")
	assert.NotContains(t, body, ">4</a>")
	assert.NotContains(t, body, ">5</a>")

	// On 2024/01/05: animal 2's outtake (01/05) still counts that day;
	// animals 1, 3, 4 are in care too → 4.
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/snapshot?snapshotDate=2024-01-05", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Animals in care on 2024-01-05: <strong>4</strong>")

	// On 2024/03/01: animal 1's outtake day → still present; animals 3, 4
	// too → 3. Animal 2 left on 01/05.
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/snapshot?snapshotDate=2024-03-01", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	body = rec.Body.String()
	assert.Contains(t, body, "Animals in care on 2024-03-01: <strong>3</strong>")
	assert.Contains(t, body, "center-b")

	// After all outtakes (2024/03/02): only the two never-outtaken animals.
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/snapshot?snapshotDate=2024-03-02", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Animals in care on 2024-03-02: <strong>2</strong>")
}

func TestSnapshot_DefaultToday_InvalidDate_Scope(t *testing.T) {
	seedRegisterDateFixtures(t, testDB)
	app := newRegisterTestApp(testDB)

	// Default date = today: 2023/2024 animals all outtaken or gone → only
	// the two never-outtaken animals remain.
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/snapshot", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Animals in care on "+time.Now().Format("2006-01-02"))

	// Invalid date → 500 (handler error).
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/snapshot?snapshotDate=not-a-date", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	// Scoped snapshot: only center-a rows.
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/snapshot?snapshotDate=2024-01-03&instance_id=center-a", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Animals in care on 2024-01-03: <strong>2</strong>")

	// Unknown instance → 404.
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/snapshot?snapshotDate=2024-01-03&instance_id=center-missing", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSnapshot_ExportCSV(t *testing.T) {
	seedRegisterDateFixtures(t, testDB)
	app := newRegisterTestApp(testDB)

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/snapshot/export.csv?snapshotDate=2024-01-03", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "registersnapshot-2024-01-03.csv")

	parsed := csv.NewReader(strings.NewReader(rec.Body.String()))
	parsed.Comma = ';'
	records, err := parsed.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 3) // header + 2 animals
	assert.Equal(t, []string{"\ufeffInstance", "Number", "Type", "Species", "Identification",
		"Zone", "Cage", "Entry Date", "Discovery Location", "Age", "Reason"}, records[0])

	// Creaves-style date format is also accepted.
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/reports/snapshot/export.csv?snapshotDate=2024/01/03&instance_id=center-a", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "registersnapshot-2024-01-03-center-a.csv")
}
