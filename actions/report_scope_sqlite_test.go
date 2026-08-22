//go:build sqlite
// +build sqlite

package actions

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"creaves-console/models"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
)

func newReportScopeTestApp(tx *pop.Connection) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/reports/by_type/{instance_id}", ReportsByType)
	return app
}

func TestReports_UnknownInstanceScoped404(t *testing.T) {
	tx := setupTest(t)
	seen := time.Now().UTC()
	require.NoError(t, tx.Create(&models.CreavesInstance{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-known", Name: "Known",
		FirstSeenAt: seen, LastSeenAt: seen,
	}))

	app := newReportScopeTestApp(tx)
	req := httptest.NewRequest(http.MethodGet, "/reports/by_type/center-missing", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}
