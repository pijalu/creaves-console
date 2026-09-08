//go:build sqlite
// +build sqlite

package actions

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"creaves-console/models"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCSVReportsTestApp mounts the /reports/csv route against the shared
// testDB, behind the real SetCurrentUser + Authorize middleware chain, so
// both anonymous and authenticated flows are exercised end to end.
func newCSVReportsTestApp(t *testing.T, tx *pop.Connection, user *models.User) *buffalo.App {
	t.Helper()
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			if user != nil {
				c.Session().Set("current_user_id", user.ID)
			}
			return next(c)
		}
	})
	app.Use(SetCurrentUser)
	app.Use(Authorize)
	app.GET("/reports/csv", ReportsCSVIndex)
	return app
}

// TestReportsCSVIndexRequiresLogin asserts anonymous visitors are redirected
// to the login page (Authorize middleware).
func TestReportsCSVIndexRequiresLogin(t *testing.T) {
	app := newCSVReportsTestApp(t, testDB, nil)

	res := httptest.NewRecorder()
	app.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/reports/csv", nil))

	assert.Equal(t, http.StatusFound, res.Code, "anonymous must be redirected")
	assert.Equal(t, "/auth/new", res.Header().Get("Location"))
}

// TestReportsCSVIndexListsAllReports asserts a logged-in user sees the four
// CSV report cards, each with its online view link and CSV download link.
func TestReportsCSVIndexListsAllReports(t *testing.T) {
	user := createConsoleTestUser(t, "csvviewer", false, true)
	app := newCSVReportsTestApp(t, testDB, user)

	res := httptest.NewRecorder()
	app.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/reports/csv", nil))
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	body := res.Body.String()

	// The four report titles.
	for _, title := range []string{
		"Consolidated Animals",
		"Register Table",
		"Year-End Snapshot",
		"Annual Report",
	} {
		assert.Contains(t, body, title)
	}

	// Online view links.
	for _, link := range []string{
		`href="/consolidated_animals"`,
		`href="/reports/register"`,
		`href="/reports/snapshot"`,
		`href="/reports/annual"`,
	} {
		assert.Contains(t, body, link, "missing online view link %s", link)
	}

	// CSV download links.
	for _, link := range []string{
		`href="/consolidated_animals/export.csv"`,
		`href="/reports/register/export.csv"`,
		`href="/reports/snapshot/export.csv"`,
		`href="/reports/annual/export.csv"`,
	} {
		assert.Contains(t, body, link, "missing CSV download link %s", link)
	}

	// Nav dropdown of the layout must expose the General and Exports entries.
	assert.Contains(t, body, `href="/reports/csv"`)
	assert.Contains(t, body, `href="/export/reports"`)
}
