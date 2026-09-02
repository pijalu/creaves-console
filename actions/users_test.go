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
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAuthLandingTestApp builds a minimal Buffalo app that injects the shared
// testDB as the request-scoped "tx" value and optionally fakes a signed-in
// user on the context, then mounts GET /auth.
func newAuthLandingTestApp(tx *pop.Connection, signedIn bool) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			if signedIn {
				c.Set("current_user", signedInUser())
			}
			return next(c)
		}
	})
	app.GET("/auth", AuthLanding)
	return app
}

// signedInUser returns the fake user placed on the context by
// newAuthLandingTestApp when a signed-in caller is simulated.
func signedInUser() *models.User {
	return &models.User{ID: uuid.Must(uuid.NewV4()), Login: "tester", Name: "Test User"}
}

func TestAuthLandingAnonymousRedirectsToSignIn(t *testing.T) {
	app := newAuthLandingTestApp(testDB, false)

	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)

	require.Equal(t, http.StatusFound, res.Code, "body: %s", res.Body.String())
	assert.Equal(t, "/auth/new", res.Header().Get("Location"))
}

func TestAuthLandingSignedInRedirectsToDashboard(t *testing.T) {
	app := newAuthLandingTestApp(testDB, true)

	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)

	require.Equal(t, http.StatusFound, res.Code, "body: %s", res.Body.String())
	assert.Equal(t, "/dashboard", res.Header().Get("Location"))
}
