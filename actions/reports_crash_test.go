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
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newReportsTestApp builds a minimal Buffalo app injecting the shared testDB
// and mounting the report pages under test.
func newReportsTestApp(tx *pop.Connection) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/reports", ReportsIndex)
	app.GET("/reports/by_location", ReportsByLocation)
	app.GET("/reports/by_type", ReportsByType)
	app.GET("/reports/by_species", ReportsBySpecies)
	return app
}

func getReportsPage(t *testing.T, app *buffalo.App, path string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest("GET", path, nil)
	require.NoError(t, err)
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	return res
}

// divBalance reports the difference between opening and closing <div> tags;
// a rendered page must be balanced (0).
func divBalance(body string) int {
	return strings.Count(body, "<div") - strings.Count(body, "</div>")
}

func TestReportsIndexStructure(t *testing.T) {
	app := newReportsTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	res := getReportsPage(t, app, "/reports")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	body := res.Body.String()

	assert.Equal(t, 0, divBalance(body), "reports index HTML must have balanced <div> tags")
	assert.Contains(t, body, "By Status")
	assert.Contains(t, body, "By Year")
	assert.Contains(t, body, "Top 20 Species")
	assert.Contains(t, body, "Top 20 Cities")
}

func TestReportsBySpeciesRenders(t *testing.T) {
	app := newReportsTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	// Global scope, no year filter.
	res := getReportsPage(t, app, "/reports/by_species")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	body := res.Body.String()
	assert.Contains(t, body, "Hérisson", "results table must list species")
	assert.Contains(t, body, `value="2024"`, "year dropdown must list years")

	// Year filter with instance scope.
	res = getReportsPage(t, app, "/reports/by_species?year=2024&instance_id=center-a")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	assert.Contains(t, res.Body.String(), "Hérisson")

	// Year filter must actually bind: center-a 2023 has only Hérisson.
	res = getReportsPage(t, app, "/reports/by_species?year=2023&instance_id=center-a")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	assert.Contains(t, res.Body.String(), "Hérisson")
	assert.NotContains(t, res.Body.String(), "<strong>Chouette</strong>", "2023 center-a has no Chouette")
	assert.NotContains(t, res.Body.String(), "<strong>Renard</strong>", "2023 center-a has no Renard")

	// Unknown instance → 404.
	res = getReportsPage(t, app, "/reports/by_species?instance_id=nope")
	assert.Equal(t, http.StatusNotFound, res.Code)
}

// TestReportsInstanceDropdownOnAllPages asserts every report page renders the
// instance selector as a dropdown ("All centers" + "Name (ID)" entries), that
// the current scope stays selected across pages, and that unknown instances
// still 404.
func TestReportsInstanceDropdownOnAllPages(t *testing.T) {
	paths := []string{
		"/reports",
		"/reports/by_location?group_by=city",
		"/reports/by_type",
		"/reports/by_species",
	}

	app := newReportsTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	for _, path := range paths {
		res := getReportsPage(t, app, path)
		require.Equal(t, http.StatusOK, res.Code, "path %s: body: %s", path, res.Body.String())
		body := res.Body.String()
		assert.Contains(t, body, `<select name="instance_id"`, "%s: instance filter must be a dropdown", path)
		assert.Contains(t, body, `>All centers<`, "%s: dropdown must offer global scope", path)
		assert.Contains(t, body, `center-a (center-a)`, "%s: dropdown entries must show name + ID", path)
		assert.Contains(t, body, `center-b (center-b)`, "%s: dropdown must list all instances", path)
	}

	// Selection persists across pages: instance_id=center-a must render as the
	// selected option on every report page.
	for _, path := range paths {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		res := getReportsPage(t, app, path+sep+"instance_id=center-a")
		require.Equal(t, http.StatusOK, res.Code, "path %s: body: %s", path, res.Body.String())
		assert.Contains(t, res.Body.String(), `<option value="center-a" selected`,
			"%s: center-a must stay selected", path)
	}

	// Unknown instance → 404 on the pages not otherwise covered.
	for _, path := range []string{"/reports", "/reports/by_type", "/reports/by_location?group_by=city"} {
		res := getReportsPage(t, app, path+"&instance_id=nope")
		assert.Equal(t, http.StatusNotFound, res.Code, "%s: unknown instance must 404", path)
	}
}

func TestReportsByLocationNullHandling(t *testing.T) {
	app := newReportsTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	now := time.Now().UTC()
	// City set, postal code NULL — city grouping scans MAX(postal_code).
	withCity := &models.ConsolidatedAnimal{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: 99,
		Year: 2024, YearNumber: 99, CurrentStatus: "in_care",
		DiscoveryCity: nullsString("Strasbourg"),
		LastEventAt:   now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, testDB.Create(withCity))
	// Postal code set, city NULL — postal grouping scans MAX(city).
	withPostal := &models.ConsolidatedAnimal{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: 98,
		Year: 2024, YearNumber: 98, CurrentStatus: "in_care",
		DiscoveryPostalCode: nullsString("67000"),
		LastEventAt:         now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, testDB.Create(withPostal))

	res := getReportsPage(t, app, "/reports/by_location?group_by=city")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	assert.Contains(t, res.Body.String(), "Strasbourg")

	res = getReportsPage(t, app, "/reports/by_location?group_by=postal_code")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	assert.Contains(t, res.Body.String(), "67000")
}
