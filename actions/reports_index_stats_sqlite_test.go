//go:build sqlite
// +build sqlite

package actions

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"creaves-console/models"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/nulls"
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

func TestReports_IndexStats_OutcomeGrouping(t *testing.T) {
	tx := setupTest(t)
	seen := time.Now().UTC()
	require.NoError(t, tx.Create(&models.CreavesInstance{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", Name: "Center A",
		FirstSeenAt: seen, LastSeenAt: seen,
	}))

	seed := func(animalID int, status string, rating nulls.Int, dead nulls.Bool) {
		require.NoError(t, tx.Create(&models.ConsolidatedAnimal{
			ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: animalID,
			Year: 2024, CurrentStatus: status,
			OuttakeRating: rating, OuttakeDead: dead,
		}))
	}
	// 1 in care; 1 positive outtake; 1 neutral outtake (stored rating 0);
	// 1 negative outtake wrongly marked released (producer Error-flag quirk);
	// 1 died in care without outtake.
	seed(1, "in_care", nulls.Int{}, nulls.Bool{})
	seed(2, "released", nulls.NewInt(1), nulls.NewBool(false))
	seed(3, "released", nulls.NewInt(0), nulls.NewBool(false))
	seed(4, "released", nulls.NewInt(-1), nulls.NewBool(true))
	seed(5, "died", nulls.Int{}, nulls.Bool{})

	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/reports", ReportsIndex)

	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	body := rec.Body.String()

	// stat-number order: total, in_care, released, died, positive, neutral, negative.
	var nums []string
	for _, m := range regexp.MustCompile(`<div class="stat-number">(\d+)</div>`).FindAllStringSubmatch(body, -1) {
		nums = append(nums, m[1])
	}
	require.Equal(t, []string{"5", "1", "2", "2", "1", "1", "1"}, nums,
		"total/in_care/released/died/positive/neutral/negative")
}

func TestReports_ByType_OutcomeGrouping(t *testing.T) {
	tx := setupTest(t)
	seen := time.Now().UTC()
	require.NoError(t, tx.Create(&models.CreavesInstance{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", Name: "Center A",
		FirstSeenAt: seen, LastSeenAt: seen,
	}))

	seed := func(animalID int, status string, rating nulls.Int, dead nulls.Bool) {
		require.NoError(t, tx.Create(&models.ConsolidatedAnimal{
			ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: animalID,
			Year: 2024, CurrentStatus: status, AnimalType: nulls.NewString("Oiseau"),
			OuttakeRating: rating, OuttakeDead: dead,
		}))
	}
	// One type, three animals: positive outtake, negative outtake stored as
	// released (producer quirk), died in care without outtake.
	seed(1, "released", nulls.NewInt(1), nulls.NewBool(false))
	seed(2, "released", nulls.NewInt(-1), nulls.NewBool(true))
	seed(3, "died", nulls.Int{}, nulls.Bool{})

	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/reports/by_type", ReportsByType)

	req := httptest.NewRequest(http.MethodGet, "/reports/by_type", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	body := rec.Body.String()

	// The Oiseau row must show count=3, in_care=0, released=1, died=2.
	row := regexp.MustCompile(`(?s)<td><strong>Oiseau</strong></td>(.*?)</tr>`).FindStringSubmatch(body)
	require.Len(t, row, 2, "Oiseau row not found in %s", body)
	var cols []string
	for _, m := range regexp.MustCompile(`badge badge-\w+">(\d+)<`).FindAllStringSubmatch(row[1], -1) {
		cols = append(cols, m[1])
	}
	require.Equal(t, []string{"3", "0", "1", "2"}, cols, "count/in_care/released/died")
}

func TestReports_IndexStats_EmptyRegister(t *testing.T) {
	tx := setupTest(t)
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/reports", ReportsIndex)

	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "empty register must not 500: %s", rec.Body.String())
}
