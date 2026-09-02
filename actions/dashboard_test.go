//go:build sqlite
// +build sqlite

package actions

import (
	"encoding/json"
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

// newDashboardTestApp builds a minimal Buffalo app that injects the shared
// testDB connection as the request-scoped "tx" value and mounts only the
// consolidated animals register routes.
func newDashboardTestApp(tx *pop.Connection) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/consolidated_animals", ConsolidatedAnimalsIndex)
	app.GET("/consolidated_animals/export.csv", ConsolidatedAnimalsExportCSV)
	app.GET("/consolidated_animals/{consolidated_animal_id}", ConsolidatedAnimalShow)
	app.GET("/consolidated_animals/{consolidated_animal_id}/drill_down", ConsolidatedAnimalDrillDown)
	return app
}

// seedRegisterFixtures resets the register tables and inserts two instances
// plus five consolidated animals across both instances with known distinct
// values for the searchable fields.
func seedRegisterFixtures(t *testing.T, tx *pop.Connection) {
	t.Helper()
	require.NoError(t, tx.RawQuery("DELETE FROM consolidated_animals").Exec())
	require.NoError(t, tx.RawQuery("DELETE FROM creaves_instances").Exec())

	now := time.Now().UTC()
	for _, id := range []string{"center-a", "center-b"} {
		inst := &models.CreavesInstance{
			ID: uuid.Must(uuid.NewV4()), InstanceID: id, Name: id,
			FirstSeenAt: now, LastSeenAt: now,
		}
		require.NoError(t, tx.Create(inst))
	}

	type fixture struct {
		instanceID  string
		year        int
		number      int
		species     string
		entryCause  string
		age         string
		ring        string
		outtakeType string
		status      string
		translation string
	}
	fixtures := []fixture{
		{"center-a", 2024, 1, "Hérisson", "Collision", "Adulte", "H12345", "", "in_care", ""},
		{"center-a", 2024, 2, "Chouette", "Trouvé au sol", "Juvénile", "B999", "Transfert", "released", ""},
		{"center-a", 2023, 1, "Hérisson", "Collision", "Adulte", "H678", "", "released", `{"fr":{"species":"Hérisson FR"}}`},
		{"center-b", 2024, 1, "Renard", "Collision", "Jeune", "", "", "in_care", ""},
		{"center-b", 2023, 5, "Chouette", "", "Adulte", "B12;3", "Sortie erreur", "died", ""},
	}
	for _, f := range fixtures {
		a := &models.ConsolidatedAnimal{
			ID:            uuid.Must(uuid.NewV4()),
			InstanceID:    f.instanceID,
			AnimalID:      f.number,
			Year:          f.year,
			YearNumber:    f.number,
			CurrentStatus: f.status,
			LastEventAt:   now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if f.species != "" {
			a.Species = nullsString(f.species)
		}
		if f.entryCause != "" {
			a.EntryCause = nullsString(f.entryCause)
		}
		if f.age != "" {
			a.AnimalAge = nullsString(f.age)
		}
		if f.ring != "" {
			a.Ring = nullsString(f.ring)
		}
		if f.outtakeType != "" {
			a.OuttakeType = nullsString(f.outtakeType)
		}
		if f.translation != "" {
			a.Translations = nullsString(f.translation)
		}
		require.NoError(t, tx.Create(a))
	}
}

func nullsString(s string) nulls.String {
	return nulls.NewString(s)
}

func getAnimals(t *testing.T, app *buffalo.App, query string) []models.ConsolidatedAnimal {
	t.Helper()
	req, err := http.NewRequest("GET", "/consolidated_animals?"+query, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	var animals []models.ConsolidatedAnimal
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &animals))
	return animals
}

func TestConsolidatedAnimalsFilterEntryCause(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	got := getAnimals(t, app, "entry_cause=Collision")
	require.Len(t, got, 3)
	for _, a := range got {
		assert.Equal(t, "Collision", a.EntryCause.String)
	}
}

// getAnimalsPage requests the register list page as HTML.
func getAnimalsPage(t *testing.T, app *buffalo.App, path string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest("GET", path, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/html")
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	return res
}

// TestConsolidatedAnimalsRegisterShowsInstance asserts the register list page
// renders an Instance column displaying each animal's source instance.
func TestConsolidatedAnimalsRegisterShowsInstance(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	res := getAnimalsPage(t, app, "/consolidated_animals")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	body := res.Body.String()
	assert.Contains(t, body, "<th>Instance</th>", "register table must have an Instance column")
	assert.Contains(t, body, "<td>center-a</td>", "rows must show the source instance")
	assert.Contains(t, body, "<td>center-b</td>", "rows must show the source instance")
	// Column order matches the CSV export: Instance first.
	assert.Less(t, strings.Index(body, "<th>Instance</th>"), strings.Index(body, "<th>Year</th>"),
		"Instance column must precede Year, matching the CSV header order")
}

// TestConsolidatedAnimalShowAndDrillDownShowInstance asserts the show and
// drill-down pages display the animal's source instance.
func TestConsolidatedAnimalShowAndDrillDownShowInstance(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	animals := getAnimals(t, app, "")
	require.NotEmpty(t, animals)
	id := animals[0].ID
	instance := animals[0].InstanceID

	for _, path := range []string{
		"/consolidated_animals/" + id.String(),
		"/consolidated_animals/" + id.String() + "/drill_down",
	} {
		res := getAnimalsPage(t, app, path)
		require.Equal(t, http.StatusOK, res.Code, "path %s: body: %s", path, res.Body.String())
		assert.Contains(t, res.Body.String(), "<td>"+instance+"</td>",
			"%s: page must display the animal's instance", path)
	}
}

func TestConsolidatedAnimalsFilterAnimalAge(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	got := getAnimals(t, app, "animal_age=Adulte")
	require.Len(t, got, 3)
	for _, a := range got {
		assert.Equal(t, "Adulte", a.AnimalAge.String)
	}
}

func TestConsolidatedAnimalsFilterRingPartialMatch(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	got := getAnimals(t, app, "ring=H12")
	require.Len(t, got, 1)
	assert.Equal(t, "H12345", got[0].Ring.String)

	// partial match on another prefix
	got = getAnimals(t, app, "ring=B9")
	require.Len(t, got, 1)
	assert.Equal(t, "B999", got[0].Ring.String)
}

func TestConsolidatedAnimalsFilterOuttakeType(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	got := getAnimals(t, app, "outtake_type=Transfert")
	require.Len(t, got, 1)
	assert.Equal(t, "Transfert", got[0].OuttakeType.String)
}

func TestConsolidatedAnimalsFilterScope(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	// instance scope restricts to center-a
	got := getAnimals(t, app, "instance_id=center-a")
	require.Len(t, got, 3)
	for _, a := range got {
		assert.Equal(t, "center-a", a.InstanceID)
	}

	// instance scope combined with a filter
	got = getAnimals(t, app, "instance_id=center-b&entry_cause=Collision")
	require.Len(t, got, 1)
	assert.Equal(t, "center-b", got[0].InstanceID)
	assert.Equal(t, "Renard", got[0].Species.String)
}

func TestConsolidatedAnimalsExportCSV(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	req, err := http.NewRequest("GET", "/consolidated_animals/export.csv?entry_cause=Collision", nil)
	require.NoError(t, err)
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	assert.Contains(t, res.Header().Get("Content-Type"), "text/csv")
	assert.Contains(t, res.Header().Get("Content-Disposition"), "consolidated_animals.csv")

	bom, recs := parseCSVBody(t, res.Body.String())
	assert.True(t, bom, "expected UTF-8 BOM prefix")
	require.Len(t, recs, 4, "header + 3 Collision rows")
	// header
	assert.Equal(t, "Species", recs[0][3])
	assert.Equal(t, "Entry cause", recs[0][11])
	// rows: ordered by year desc, year_number asc -> 2024#1 Hérisson, 2024#1 Renard, 2023#1 Hérisson
	assert.Equal(t, "Hérisson", recs[1][3])
	assert.Equal(t, "Collision", recs[1][11])
	assert.Equal(t, "center-a", recs[1][0])
	assert.Equal(t, "Renard", recs[2][3])
	assert.Equal(t, "center-b", recs[2][0])
}

func TestConsolidatedAnimalsExportCSVLocalized(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	req, err := http.NewRequest("GET", "/consolidated_animals/export.csv?year=2023&instance_id=center-a", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "lang", Value: "fr"})
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())

	_, recs := parseCSVBody(t, res.Body.String())
	require.Len(t, recs, 2, "header + 1 row")
	assert.Equal(t, "Espèce", recs[0][3], "header must be localized to fr")
	assert.Equal(t, "Cause d'entrée", recs[0][11], "header must be localized to fr")
	assert.Equal(t, "Hérisson FR", recs[1][3], "species must render its stored fr translation")
}

func TestConsolidatedAnimalsExportCSVEscapingAndEmpty(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	// ring "B12;3" must be quoted in output; entry_cause NULL must be empty.
	req, err := http.NewRequest("GET", "/consolidated_animals/export.csv?year=2023&instance_id=center-b", nil)
	require.NoError(t, err)
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())

	assert.Contains(t, res.Body.String(), `"B12;3"`, "ring containing ';' must be quoted")
	_, recs := parseCSVBody(t, res.Body.String())
	require.Len(t, recs, 2)
	assert.Equal(t, "", recs[1][11], "NULL entry_cause must export as empty string")
	assert.Equal(t, "Sortie erreur", recs[1][13])
}

func TestConsolidatedAnimalsExportCSVScope(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	req, err := http.NewRequest("GET", "/consolidated_animals/export.csv?instance_id=center-b", nil)
	require.NoError(t, err)
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())

	_, recs := parseCSVBody(t, res.Body.String())
	require.Len(t, recs, 3, "header + 2 center-b rows")
	for _, rec := range recs[1:] {
		assert.Equal(t, "center-b", rec[0])
	}
}
