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
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
)

func TestReports_IndexStats(t *testing.T) {
	tx := setupTest(t)
	seen := time.Now().UTC()
	require.NoError(t, tx.Create(&models.CreavesInstance{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", Name: "Center A",
		FirstSeenAt: seen, LastSeenAt: seen,
	}))
	require.NoError(t, tx.Create(&models.CreavesInstance{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-b", Name: "Center B",
		FirstSeenAt: seen, LastSeenAt: seen,
	}))

	seed := func(instanceID string, animalID int, status string) {
		require.NoError(t, tx.Create(&models.ConsolidatedAnimal{
			ID: uuid.Must(uuid.NewV4()), InstanceID: instanceID, AnimalID: animalID,
			Year: 2024, CurrentStatus: status,
		}))
	}
	// center-a: 2 in care + 1 released ; center-b: 1 in care
	seed("center-a", 1, "in_care")
	seed("center-a", 2, "in_care")
	seed("center-a", 3, "released")
	seed("center-b", 4, "in_care")

	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/reports", ReportsIndex)

	get := func(url string) string {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "GET %s -> %d: %s", url, rec.Code, rec.Body.String())
		return rec.Body.String()
	}

	stat := func(label string) string {
		return `<div class="stat-number">` + label + `</div>`
	}

	// Global scope: 4 animals, 3 in care, 1 released, 0 died
	body := get("/reports")
	require.Contains(t, body, stat("4"), "global total_animals")
	require.Contains(t, body, stat("3"), "global in_care")
	require.Contains(t, body, stat("1"), "global released")
	require.Contains(t, body, stat("0"), "global died")
	require.Contains(t, body, "color: inherit", "stat-number must not hard-code #007bff on colored cards")
	require.NotContains(t, body, "color: #007bff")

	// Instance scope: only center-a counted
	body = get("/reports?instance_id=center-a")
	require.Contains(t, body, stat("3"), "scoped total_animals")
	require.Contains(t, body, stat("2"), "scoped in_care")
	require.Contains(t, body, stat("1"), "scoped released")
	require.Contains(t, body, stat("0"), "scoped died")
}
