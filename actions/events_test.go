//go:build sqlite
// +build sqlite

package actions

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"creaves-console/models"
	"creaves-console/templates"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEventsTestApp builds a minimal Buffalo app that injects the shared
// testDB as the request-scoped "tx" value, fakes a signed-in user on the
// context, and mounts the events routes.
func newEventsTestApp(tx *pop.Connection, admin bool) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			c.Set("current_user", &models.User{ID: uuid.Must(uuid.NewV4()), Login: "admin", Admin: admin})
			return next(c)
		}
	})
	app.GET("/events", EventsIndex)
	app.GET("/events/delete", EventsDeleteNew)
	app.POST("/events/delete", EventsDeleteCreate)
	app.GET("/events/archives", EventsArchivesIndex)
	app.GET("/events/archives/{archive_id}/download", EventsArchiveDownload)
	app.GET("/events/{event_id}", EventShow)
	return app
}

// seedEventsFixtures resets the event tables and inserts events for two
// instances: two processed + one pending for "center-a" (the pending one
// carries a rich payload), one processed for "center-b" (scoping sentinel).
func seedEventsFixtures(t *testing.T, tx *pop.Connection) {
	t.Helper()
	require.NoError(t, tx.RawQuery("DELETE FROM consolidated_animals").Exec())
	require.NoError(t, tx.RawQuery("DELETE FROM event_streams").Exec())

	now := time.Now().UTC()
	processed := now.Add(-time.Hour)
	payload := `{"animal":{"id":42,"year":2024,"species":"Hérisson"},"timestamp":"2026-09-04T10:00:00Z"}`

	for _, tc := range []struct {
		instance  string
		animalID  int
		eventType models.EventType
		processed *time.Time
		payload   string
	}{
		{"center-a", 42, models.EventTypeAnimalDiscovered, &processed, payload},
		{"center-a", 42, models.EventTypeAnimalStatusChanged, &processed, `{}`},
		{"center-a", 43, models.EventTypeAnimalReleased, nil, `{}`},
		{"center-b", 7, models.EventTypeAnimalDied, &processed, `{}`},
	} {
		e := &models.EventStream{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: tc.instance,
			AnimalID:   tc.animalID,
			EventType:  tc.eventType,
			Payload:    []byte(tc.payload),
			ImportedAt: now,
			ProcessedAt: func() *time.Time {
				if tc.processed == nil {
					return nil
				}
				p := *tc.processed
				return &p
			}(),
		}
		require.NoError(t, tx.Create(e))
	}

	// A consolidated animal so the show page can link from the event.
	a := &models.ConsolidatedAnimal{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: 42,
		Year: 2024, YearNumber: 1, Species: nulls.NewString("Hérisson"),
	}
	require.NoError(t, tx.Create(a))
}

func getEventsPage(t *testing.T, app *buffalo.App, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	return res
}

func TestEventsIndexForbiddenForNonAdmin(t *testing.T) {
	seedEventsFixtures(t, testDB)
	app := newEventsTestApp(testDB, false)

	res := getEventsPage(t, app, "/events")
	assert.Equal(t, http.StatusForbidden, res.Code)
}

func TestEventsIndexListsEvents(t *testing.T) {
	seedEventsFixtures(t, testDB)
	app := newEventsTestApp(testDB, true)

	res := getEventsPage(t, app, "/events")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	body := res.Body.String()

	assert.Contains(t, body, "center-a", "index must list events of center-a")
	assert.Contains(t, body, "center-b", "index must list events of center-b")
	assert.Contains(t, body, "animal_discovered", "index must show the event type")
	assert.Contains(t, body, "Processed", "index must show the processed badge")
	assert.Contains(t, body, "Pending", "index must show the pending badge")
	assert.Contains(t, body, "/events/", "index must link to the detail pages")
	assert.Contains(t, body, "/instances/center-a", "instance column must link to the instance page")
}

func TestEventsIndexFilters(t *testing.T) {
	seedEventsFixtures(t, testDB)
	app := newEventsTestApp(testDB, true)

	// Row-level assertions use the animal-id cells (<td>42</td> etc.) so the
	// filter dropdown options (which list every event type) cannot interfere.

	res := getEventsPage(t, app, "/events?instance_id=center-a")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	assert.Contains(t, res.Body.String(), "<td>43</td>", "instance filter must keep center-a events")
	assert.NotContains(t, res.Body.String(), "<td>7</td>", "instance filter must hide center-b events")

	res = getEventsPage(t, app, "/events?event_type=animal_released")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	assert.Contains(t, res.Body.String(), "<td>43</td>", "type filter must keep the matching row")
	assert.NotContains(t, res.Body.String(), "<td>42</td>", "type filter must hide other types")

	res = getEventsPage(t, app, "/events?processed=pending")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	assert.Contains(t, res.Body.String(), "<td>43</td>", "pending filter must keep the unprocessed event")
	assert.NotContains(t, res.Body.String(), "<td>42</td>", "pending filter must hide processed events")

	res = getEventsPage(t, app, "/events?processed=processed")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	assert.Contains(t, res.Body.String(), "<td>42</td>", "processed filter must keep processed events")
	assert.NotContains(t, res.Body.String(), "<td>43</td>", "processed filter must hide the pending event")
}

func TestEventShowRendersPayloadAndLinksAnimal(t *testing.T) {
	seedEventsFixtures(t, testDB)
	app := newEventsTestApp(testDB, true)

	e := &models.EventStream{}
	require.NoError(t, testDB.Where("instance_id = ? AND animal_id = ?", "center-a", 42).First(e))

	res := getEventsPage(t, app, "/events/"+e.ID.String())
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	body := res.Body.String()

	assert.Contains(t, body, e.ID.String(), "show page must render the event id")
	assert.Contains(t, body, "Hérisson", "show page must render the pretty-printed payload")
	assert.Contains(t, body, "/consolidated_animals/", "show page must link to the consolidated animal")

	// Event for animal 43 has no consolidated animal — no link, no failure.
	e43 := &models.EventStream{}
	require.NoError(t, testDB.Where("animal_id = ?", 43).First(e43))
	res43 := getEventsPage(t, app, "/events/"+e43.ID.String())
	require.Equal(t, http.StatusOK, res43.Code, "body: %s", res43.Body.String())
	assert.Contains(t, res43.Body.String(), "No consolidated animal", "missing animal must be explained")
}

func TestEventShowUnknownEventNotFound(t *testing.T) {
	seedEventsFixtures(t, testDB)
	app := newEventsTestApp(testDB, true)

	res := getEventsPage(t, app, "/events/"+uuid.Must(uuid.NewV4()).String())
	assert.Equal(t, http.StatusNotFound, res.Code)
}

func TestEventsLocaleTemplatesAreSelfContained(t *testing.T) {
	for _, locale := range []string{"", ".fr", ".de", ".nl"} {
		for _, name := range []string{"index", "show"} {
			path := "events/" + name + ".plush" + locale + ".html"
			body, err := fs.ReadFile(templates.FS(), path)
			require.NoError(t, err, "locale template %s must be embedded", path)
			assert.NotContains(t, string(body), `partial("`, "locale template %s must not depend on partials", path)
			assert.Contains(t, string(body), "event.ID", "locale template %s must render event data", path)
		}
		indexPath := "events/index.plush" + locale + ".html"
		body, err := fs.ReadFile(templates.FS(), indexPath)
		require.NoError(t, err, "locale template %s must be embedded", indexPath)
		assert.Contains(t, string(body), "/instances/", "locale template %s must link to instances", indexPath)
		assert.Contains(t, string(body), "/events/", "locale template %s must link to event details", indexPath)
	}

	// The admin navigation must link the new view in every locale.
	for _, locale := range []string{"", ".fr", ".de", ".nl"} {
		path := "application.plush" + locale + ".html"
		body, err := fs.ReadFile(templates.FS(), path)
		require.NoError(t, err, "locale template %s must be embedded", path)
		assert.Contains(t, string(body), `href="/events"`, "locale template %s must link to the events view", path)
	}
}

// TestEventsIndexPagination exercises the paginator wiring: per_page=2 must
// split the four seeded events across two pages.
func TestEventsIndexPagination(t *testing.T) {
	seedEventsFixtures(t, testDB)
	app := newEventsTestApp(testDB, true)

	res := getEventsPage(t, app, "/events?page=1&per_page=2")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	assert.Contains(t, res.Body.String(), "pagination", "index must wire the paginator")
}
