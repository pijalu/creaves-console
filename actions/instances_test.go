//go:build sqlite
// +build sqlite

package actions

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

// newInstancesTestApp builds a minimal Buffalo app that injects the shared
// testDB as the request-scoped "tx" value, fakes a signed-in admin on the
// context, and mounts the admin instances routes.
func newInstancesTestApp(tx *pop.Connection, admin bool) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			c.Set("current_user", &models.User{ID: uuid.Must(uuid.NewV4()), Login: "admin", Admin: admin})
			return next(c)
		}
	})
	app.GET("/instances", InstancesIndex)
	app.POST("/instances/{instance_id}/cleanup", InstanceCleanup)
	return app
}

// seedInstanceCleanupFixtures resets the admin tables and inserts two
// instances; "center-a" gets known animals/events/keys counts, "center-b"
// keeps a sentinel row per table so the purge test can prove scoping.
func seedInstanceCleanupFixtures(t *testing.T, tx *pop.Connection) {
	t.Helper()
	require.NoError(t, tx.RawQuery("DELETE FROM consolidated_animals").Exec())
	require.NoError(t, tx.RawQuery("DELETE FROM event_streams").Exec())
	require.NoError(t, tx.RawQuery("DELETE FROM webhook_api_keys").Exec())
	require.NoError(t, tx.RawQuery("DELETE FROM creaves_instances").Exec())

	now := time.Now().UTC()
	for _, id := range []string{"center-a", "center-b"} {
		inst := &models.CreavesInstance{
			ID: uuid.Must(uuid.NewV4()), InstanceID: id, Name: id,
			FirstSeenAt: now, LastSeenAt: now,
		}
		require.NoError(t, tx.Create(inst))
	}
	for _, instanceID := range []string{"center-a", "center-b"} {
		for i := 0; i < 2; i++ {
			a := &models.ConsolidatedAnimal{
				ID: uuid.Must(uuid.NewV4()), InstanceID: instanceID,
				Year: 2024, YearNumber: i + 1, Species: nulls.NewString("Hérisson"),
			}
			require.NoError(t, tx.Create(a))
		}
		for i := 0; i < 3; i++ {
			e := &models.EventStream{
				ID: uuid.Must(uuid.NewV4()), InstanceID: instanceID,
				AnimalID: i + 1, EventType: models.EventTypeAnimalDiscovered,
				Payload: []byte(`{}`), SourceDB: instanceID, ImportedAt: now,
			}
			require.NoError(t, tx.Create(e))
		}
		k := &models.WebhookAPIKey{
			ID: uuid.Must(uuid.NewV4()), Name: "key-" + instanceID,
			KeyHash: "hash-" + instanceID, KeyPrefix: "ck_" + instanceID[:2],
			InstanceID: instanceID, Active: true,
		}
		require.NoError(t, tx.Create(k))
	}
}

func TestInstanceShowLocaleTemplatesAreSelfContained(t *testing.T) {
	for _, locale := range []string{"", ".fr", ".de", ".nl"} {
		path := "instances/show.plush" + locale + ".html"
		body, err := fs.ReadFile(templates.FS(), path)
		require.NoError(t, err, "locale template %s must be embedded", locale)
		assert.NotContains(t, string(body), `partial("instances/show.plush.html")`, "locale template %s must not depend on missing partial", locale)
		assert.Contains(t, string(body), "instance.InstanceID", "locale template %s must render instance data", locale)
	}
}

func TestInstancesIndexShowsDeleteDialogWithCounts(t *testing.T) {
	seedInstanceCleanupFixtures(t, testDB)
	app := newInstancesTestApp(testDB, true)

	req := httptest.NewRequest(http.MethodGet, "/instances", nil)
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)

	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	body := res.Body.String()
	assert.Contains(t, body, `id="delete-instance-modal"`, "index must embed the shared confirmation dialog")
	assert.Contains(t, body, `name="instance_id_confirmation"`, "dialog must require a typed confirmation")
	assert.Contains(t, body, `data-instance-id="center-a"`, "delete button must carry the instance id")
	assert.Contains(t, body, `data-animals="2"`, "dialog data must expose the animal count")
	assert.Contains(t, body, `data-events="3"`, "dialog data must expose the event count")
	assert.Contains(t, body, `data-keys="1"`, "dialog data must expose the api key count")
}

func TestInstancesIndexForbiddenForNonAdmin(t *testing.T) {
	seedInstanceCleanupFixtures(t, testDB)
	app := newInstancesTestApp(testDB, false)

	req := httptest.NewRequest(http.MethodGet, "/instances", nil)
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)

	assert.Equal(t, http.StatusForbidden, res.Code)
}

func postCleanup(t *testing.T, app *buffalo.App, instanceID, confirmation string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	if confirmation != "" {
		form.Set("instance_id_confirmation", confirmation)
	}
	req := httptest.NewRequest(http.MethodPost, "/instances/"+instanceID+"/cleanup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	return res
}

func assertCounts(t *testing.T, tx *pop.Connection, instanceID string, animals, events, keys, registry int) {
	t.Helper()
	a, err := tx.Where("instance_id = ?", instanceID).Count(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	e, err := tx.Where("instance_id = ?", instanceID).Count(&models.EventStream{})
	require.NoError(t, err)
	k, err := tx.Where("instance_id = ?", instanceID).Count(&models.WebhookAPIKey{})
	require.NoError(t, err)
	inst, err := tx.Where("instance_id = ?", instanceID).Count(&models.CreavesInstance{})
	require.NoError(t, err)
	assert.Equal(t, animals, a, "animals for %s", instanceID)
	assert.Equal(t, events, e, "events for %s", instanceID)
	assert.Equal(t, keys, k, "keys for %s", instanceID)
	assert.Equal(t, registry, inst, "registry rows for %s", instanceID)
}

func TestInstanceCleanupRequiresTypedConfirmation(t *testing.T) {
	seedInstanceCleanupFixtures(t, testDB)
	app := newInstancesTestApp(testDB, true)

	for _, confirmation := range []string{"", "center-b"} {
		res := postCleanup(t, app, "center-a", confirmation)
		require.Equal(t, http.StatusUnprocessableEntity, res.Code,
			"confirmation %q must be rejected: body: %s", confirmation, res.Body.String())
	}
	// Surrounding whitespace is trimmed by the handler before comparing.
	res := postCleanup(t, app, "center-a", " center-a ")
	require.Equal(t, http.StatusSeeOther, res.Code, "body: %s", res.Body.String())
	assertCounts(t, testDB, "center-a", 0, 0, 0, 0)
}

func TestInstanceCleanupPurgesOnlyTargetInstance(t *testing.T) {
	seedInstanceCleanupFixtures(t, testDB)
	app := newInstancesTestApp(testDB, true)

	res := postCleanup(t, app, "center-a", "center-a")
	require.Equal(t, http.StatusSeeOther, res.Code, "body: %s", res.Body.String())
	assert.Equal(t, "/instances", res.Header().Get("Location"))

	assertCounts(t, testDB, "center-a", 0, 0, 0, 0)
	// Other instance must be untouched.
	assertCounts(t, testDB, "center-b", 2, 3, 1, 1)
}

func TestInstanceCleanupUnknownInstanceNotFound(t *testing.T) {
	seedInstanceCleanupFixtures(t, testDB)
	app := newInstancesTestApp(testDB, true)

	res := postCleanup(t, app, "ghost", "ghost")
	assert.Equal(t, http.StatusNotFound, res.Code)
}
