//go:build sqlite
// +build sqlite

package actions

// Cross-page consistency test for bugs.md item 8 (MEDIUM): the animal totals
// shown on the dashboard, the reports index, the instance admin view and the
// sync management screen must all come from one shared stats source and must
// therefore agree for a given instant. The four screens are rendered against
// the exact same database snapshot and their numbers are compared.

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"creaves-console/models"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newStatsConsistencyTestApp mounts all four screens under one app with a
// signed-in admin so the whole page set can be rendered from one snapshot.
func newStatsConsistencyTestApp(tx *pop.Connection) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			c.Set("current_user", &models.User{ID: uuid.Must(uuid.NewV4()), Login: "admin", Admin: true})
			return next(c)
		}
	})
	app.GET("/", DashboardIndex)
	app.GET("/reports", ReportsIndex)
	app.GET("/instances", InstancesIndex)
	app.GET("/sync_management", SyncManagementIndex)
	app.GET("/consolidated_animals", ConsolidatedAnimalsIndex)
	return app
}

func fetchPage(t *testing.T, app *buffalo.App, path string) string {
	t.Helper()
	req, err := http.NewRequest("GET", path, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/html")
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, "GET %s failed: %s", path, res.Body.String())
	return res.Body.String()
}

// statCardNumbers extracts the values of all stat-number cards in document
// order (dashboard: animals, events, instances, keys).
var statCardNumbers = regexp.MustCompile(`stat-number">\s*(\d+)\s*<`)

// cellNumbers extracts the integers that form the complete content of a table
// cell (dates or text cells never match).
var cellNumbers = regexp.MustCompile(`<td[^>]*>\s*(\d+)\s*</td>`)

// rowNumbers returns every standalone numeric cell of the table row that
// contains the needle.
func rowNumbers(t *testing.T, body, needle string) []int {
	t.Helper()
	row := extractRowContaining(t, body, needle)
	matches := cellNumbers.FindAllStringSubmatch(row, -1)
	nums := make([]int, 0, len(matches))
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		require.NoError(t, err)
		nums = append(nums, n)
	}
	return nums
}

func containsInt(nums []int, want int) bool {
	for _, n := range nums {
		if n == want {
			return true
		}
	}
	return false
}

// TestAnimalTotalsConsistentAcrossScreens renders dashboard, reports index,
// instances index, sync management and the register from the same fixture
// snapshot and asserts every screen shows the same totals.
func TestAnimalTotalsConsistentAcrossScreens(t *testing.T) {
	app := newStatsConsistencyTestApp(testDB)
	seedRegisterFixtures(t, testDB)
	seedDeceasedReleasedAnimal(t, testDB)

	// Known fixture totals: seedRegisterFixtures inserts 3 center-a + 2
	// center-b rows, plus 1 deceased center-a row = 6 animals.
	const wantTotal = 6
	const wantCenterA = 4
	const wantCenterB = 2

	dash := fetchPage(t, app, "/")
	reports := fetchPage(t, app, "/reports")
	instances := fetchPage(t, app, "/instances")
	sync := fetchPage(t, app, "/sync_management")

	// Dashboard stat card #1 (Total Animals) and reports stat card #1 must
	// show the same grand total.
	dashNums := statCardNumbers.FindAllStringSubmatch(dash, -1)
	repNums := statCardNumbers.FindAllStringSubmatch(reports, -1)
	require.GreaterOrEqual(t, len(dashNums), 1, "dashboard must render stat cards: %s", dash)
	require.GreaterOrEqual(t, len(repNums), 1, "reports must render stat cards: %s", reports)
	dashTotal, err := strconv.Atoi(dashNums[0][1])
	require.NoError(t, err)
	repTotal, err := strconv.Atoi(repNums[0][1])
	require.NoError(t, err)
	assert.Equal(t, wantTotal, dashTotal, "dashboard Total Animals")
	assert.Equal(t, wantTotal, repTotal, "reports Total Animals")

	// Per-instance counts must agree between the dashboard by-instance
	// table, the instances admin view and the sync management rows.
	// Scope to the by-instance table rows: the instance filter dropdown also
	// lists ">center-a<"-style option labels outside any <tr>.
	dashA := rowNumbers(t, dash, "<td>center-a</td>")
	dashB := rowNumbers(t, dash, "<td>center-b</td>")
	syncA := rowNumbers(t, sync, ">center-a<")
	syncB := rowNumbers(t, sync, ">center-b<")
	instA := rowNumbers(t, instances, "center-a")
	instB := rowNumbers(t, instances, "center-b")

	assert.True(t, containsInt(dashA, wantCenterA), "dashboard center-a count %v", dashA)
	assert.True(t, containsInt(dashB, wantCenterB), "dashboard center-b count %v", dashB)
	assert.True(t, containsInt(syncA, wantCenterA), "sync_management center-a count %v", syncA)
	assert.True(t, containsInt(syncB, wantCenterB), "sync_management center-b count %v", syncB)
	assert.True(t, containsInt(instA, wantCenterA), "instances view center-a count %v", instA)
	assert.True(t, containsInt(instB, wantCenterB), "instances view center-b count %v", instB)

	// Sync management grand total (registered + orphaned) must equal the
	// dashboard/reports total — there are no orphaned fixtures.
	assert.Contains(t, sync, "Total animals in database:")
	assert.Regexp(t, `Total animals in database:</strong>\s*`+strconv.Itoa(wantTotal), sync,
		"sync_management grand total must equal %d", wantTotal)

	// Register pagination total must agree too: no filters paginate the
	// full register.
	register := fetchPage(t, app, "/consolidated_animals")
	assert.True(t, strings.Contains(register, strconv.Itoa(wantTotal)),
		"register pagination info must reference the same total %d", wantTotal)
}
