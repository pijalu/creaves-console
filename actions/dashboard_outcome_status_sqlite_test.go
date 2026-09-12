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
	"github.com/gobuffalo/nulls"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTallyOutcomes_InCareAndDeceasedClassification pins the outcome-based
// classification shared by the dashboard and the reports index (bugs.md
// "console does not show deceased count animals"):
//   - a released animal with a negative outtake rating counts as DIED, not
//     released (legacy rows carry current_status='released' for deceased
//     animals);
//   - an animal that died in care without any outtake counts as DIED via the
//     current_status fallback;
//   - a plain in-care animal counts only as in_care.
func TestTallyOutcomes_InCareAndDeceasedClassification(t *testing.T) {
	tx := setupTest(t)
	now := time.Now().UTC()

	seed := func(animalID int, status string, rating *int, dead *bool) {
		a := &models.ConsolidatedAnimal{
			ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: animalID,
			Year: 2024, CurrentStatus: status,
			LastEventAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if rating != nil {
			a.OuttakeRating = nulls.NewInt(*rating)
		}
		if dead != nil {
			a.OuttakeDead = nulls.NewBool(*dead)
		}
		require.NoError(t, tx.Create(a))
	}

	neg := -1
	pos := 1
	f := false
	seed(1, "in_care", nil, nil)          // plain in care
	seed(2, "released", &pos, &f)         // released, positive outcome
	seed(3, "released", &neg, nil)        // deceased but stored as released (legacy)
	seed(4, "died", nil, nil)             // died in care, no outtake
	seed(5, "released", nil, &f)          // released, no rating → released fallback
	seed(6, "died", &neg, nil)            // negative outcome under died

	tally, err := tallyOutcomes(tx, "", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, tally.InCare, "in care")
	assert.Equal(t, 2, tally.Released, "released (positive + no-rating fallback)")
	assert.Equal(t, 3, tally.Died, "died (legacy released w/ negative rating + died in care + died w/ negative rating)")
	assert.Equal(t, 2, tally.Negative, "negative outcomes")
	assert.Equal(t, 1, tally.Positive, "positive outcomes")
}

// TestDashboard_IndexOutcomeStatusCounts proves the dashboard "Animals by
// Status" table uses the outcome classification: the deceased count must
// match /reports (bugs.md: dashboard showed 1 deceased while reports showed
// the real deceased count).
func TestDashboard_IndexOutcomeStatusCounts(t *testing.T) {
	tx := setupTest(t)
	now := time.Now().UTC()
	require.NoError(t, tx.Create(&models.CreavesInstance{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", Name: "Center A",
		FirstSeenAt: now, LastSeenAt: now,
	}))

	neg := -1
	seed := func(animalID int, status string, rating *int) {
		a := &models.ConsolidatedAnimal{
			ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: animalID,
			Year: 2024, CurrentStatus: status,
			LastEventAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if rating != nil {
			a.OuttakeRating = nulls.NewInt(*rating)
		}
		require.NoError(t, tx.Create(a))
	}
	// 2 in care, 1 plain released, 2 deceased (one stored as released with a
	// negative rating — the exact legacy shape the raw current_status
	// grouping miscounted).
	seed(1, "in_care", nil)
	seed(2, "in_care", nil)
	seed(3, "released", nil)
	seed(4, "released", &neg)
	seed(5, "released", &neg)

	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/", DashboardIndex)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	body := rec.Body.String()

	section := statusSection(t, body)
	assertStatusRow(t, section, "In care", "2")
	assertStatusRow(t, section, "Released", "1")
	assertStatusRow(t, section, "Died", "2")
}

// statusSection cuts the body down to the "Animals by Status" card so
// count assertions cannot hit numbers from other widgets.
func statusSection(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "Animals by Status")
	require.GreaterOrEqual(t, start, 0, "status card header missing")
	rest := body[start:]
	end := strings.Index(rest, "Animals by Instance")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

// assertStatusRow checks the status table row for label shows the expected
// count in its count cell.
func assertStatusRow(t *testing.T, section, label, want string) {
	t.Helper()
	idx := strings.Index(section, label)
	require.GreaterOrEqual(t, idx, 0, "label %q not found in section", label)
	rowStart := strings.LastIndex(section[:idx], "<tr>")
	require.GreaterOrEqual(t, rowStart, 0, "row start for %q", label)
	rowEnd := strings.Index(section[idx:], "</tr>")
	require.GreaterOrEqual(t, rowEnd, 0, "row end for %q", label)
	row := section[rowStart : idx+rowEnd+5]
	require.Contains(t, row, `>`+want+`</td>`, "status row for %q", label)
}
