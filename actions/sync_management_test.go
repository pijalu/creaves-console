//go:build sqlite
// +build sqlite

package actions

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"creaves-console/models"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSyncManagementTestApp builds a minimal Buffalo app that injects the
// shared testDB as the request-scoped "tx" value, fakes a signed-in user
// (admin or not) on the context, and mounts the sync management routes.
func newSyncManagementTestApp(tx *pop.Connection, admin bool) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			c.Set("current_user", &models.User{ID: uuid.Must(uuid.NewV4()), Login: "admin", Admin: admin})
			return next(c)
		}
	})
	app.GET("/sync_management", SyncManagementIndex)
	app.POST("/sync_management/delete-all-animals", SyncManagementDeleteAllAnimals)
	app.POST("/sync_management/delete-instance-animals", SyncManagementDeleteInstanceAnimals)
	return app
}

func postForm(app *buffalo.App, target string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func countAnimals(t *testing.T, tx *pop.Connection, instanceID string) int {
	t.Helper()
	var count int
	if instanceID == "" {
		require.NoError(t, tx.RawQuery("SELECT COUNT(*) FROM consolidated_animals").First(&count))
	} else {
		require.NoError(t, tx.RawQuery("SELECT COUNT(*) FROM consolidated_animals WHERE instance_id = ?", instanceID).First(&count))
	}
	return count
}

func TestSyncManagementIndex_AdminSeesCounts(t *testing.T) {
	seedInstanceCleanupFixtures(t, testDB)
	app := newSyncManagementTestApp(testDB, true)

	rec := perform(app, http.MethodGet, "/sync_management")
	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	// 2 animals per instance, 4 total.
	assert.Contains(t, body, "center-a")
	assert.Contains(t, body, "center-b")
}

func TestSyncManagementIndex_NonAdminDenied(t *testing.T) {
	seedInstanceCleanupFixtures(t, testDB)
	app := newSyncManagementTestApp(testDB, false)

	rec := perform(app, http.MethodGet, "/sync_management")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSyncManagementDeleteInstanceAnimals_Scoped(t *testing.T) {
	seedInstanceCleanupFixtures(t, testDB)
	app := newSyncManagementTestApp(testDB, true)

	require.Equal(t, 2, countAnimals(t, testDB, "center-a"))
	require.Equal(t, 2, countAnimals(t, testDB, "center-b"))

	rec := postForm(app, "/sync_management/delete-instance-animals", url.Values{
		"instance_id": {"center-a"},
	})
	assert.Equal(t, http.StatusSeeOther, rec.Code)

	assert.Equal(t, 0, countAnimals(t, testDB, "center-a"), "instance A animals must be deleted")
	assert.Equal(t, 2, countAnimals(t, testDB, "center-b"), "instance B animals must be kept")

	// Events and instance registry must survive (resync can rebuild).
	var events int
	require.NoError(t, testDB.RawQuery("SELECT COUNT(*) FROM event_streams WHERE instance_id = 'center-a'").First(&events))
	assert.Equal(t, 3, events)
	var instances int
	require.NoError(t, testDB.RawQuery("SELECT COUNT(*) FROM creaves_instances WHERE instance_id = 'center-a'").First(&instances))
	assert.Equal(t, 1, instances)
}

func TestSyncManagementDeleteAllAnimals(t *testing.T) {
	seedInstanceCleanupFixtures(t, testDB)
	app := newSyncManagementTestApp(testDB, true)

	require.Equal(t, 4, countAnimals(t, testDB, ""))

	rec := postForm(app, "/sync_management/delete-all-animals", url.Values{})
	assert.Equal(t, http.StatusSeeOther, rec.Code)

	assert.Equal(t, 0, countAnimals(t, testDB, ""), "all animals must be deleted")

	// Events and instance registry must survive.
	var events int
	require.NoError(t, testDB.RawQuery("SELECT COUNT(*) FROM event_streams").First(&events))
	assert.Equal(t, 6, events)
	var instances int
	require.NoError(t, testDB.RawQuery("SELECT COUNT(*) FROM creaves_instances").First(&instances))
	assert.Equal(t, 2, instances)
}

func TestSyncManagementDelete_NonAdminDenied(t *testing.T) {
	seedInstanceCleanupFixtures(t, testDB)
	app := newSyncManagementTestApp(testDB, false)

	rec := postForm(app, "/sync_management/delete-all-animals", url.Values{})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 4, countAnimals(t, testDB, ""), "nothing must be deleted for non-admin")

	rec = postForm(app, "/sync_management/delete-instance-animals", url.Values{
		"instance_id": {"center-a"},
	})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 2, countAnimals(t, testDB, "center-a"))
}

func TestSyncManagementDeleteInstanceAnimals_NoInstance(t *testing.T) {
	seedInstanceCleanupFixtures(t, testDB)
	app := newSyncManagementTestApp(testDB, true)

	rec := postForm(app, "/sync_management/delete-instance-animals", url.Values{})
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, 4, countAnimals(t, testDB, ""), "nothing must be deleted without instance_id")
}
