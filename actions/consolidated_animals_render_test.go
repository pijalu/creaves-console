//go:build sqlite
// +build sqlite

package actions

// Rendering regression tests for bugs.md item 5 (CRITICAL UX):
//  1. The register table's Species, Identification, Discovery location, PC,
//     City and Cause cells use Plush *inline* if-expressions
//     (`<%= if (cond) { expr } else { "-" } %>`), which Plush parses but never
//     prints — every row rendered an empty cell even though the data existed.
//     These tests prove the cells render values (or "-").
//  2. The year filter compared `params["year"] == string(y.Year)`; in Plush
//     `string(int)` yields "" so the condition was always true and *every*
//     option was marked selected. These tests prove exactly one option is
//     selected when filtering and that "All" is the default when not.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsolidatedAnimalsRegisterRendersNonEmptyCells asserts the six formerly
// empty columns (Species, Identification, Discovery location, PC, City, Cause)
// actually render their values for rows that have them and "-" for rows that
// don't.
func TestConsolidatedAnimalsRegisterRendersNonEmptyCells(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	res := getAnimalsPage(t, app, "/consolidated_animals")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	body := res.Body.String()

	// Row center-a/2024/1: Species Hérisson, Ring H12345.
	assert.Contains(t, body, "Hérisson", "Species cell must render the species value")
	assert.Contains(t, body, "H12345", "Identification cell must render the ring value")
	assert.Contains(t, body, "Forêt domaniale", "Discovery location cell must render the location")
	assert.Contains(t, body, "67000", "PC cell must render the postal code")
	assert.Contains(t, body, "Strasbourg", "City cell must render the city")
	assert.Contains(t, body, "Collision", "Cause cell must render the entry cause")

	// Row center-b/2023/5 has no ring: the Identification cell must fall back
	// to "-" instead of rendering empty.
	assert.Regexp(t, `(?s)<td>\s*-\s*</td>`, body,
		"cells of animals with NULL values must render \"-\" instead of staying empty")
}

// TestConsolidatedAnimalsYearDropdownExactlyOneSelected asserts the year filter
// marks exactly the active option selected (never all of them) and defaults to
// "All" when no year filter is applied.
func TestConsolidatedAnimalsYearDropdownExactlyOneSelected(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	// Default (no year param): no year option may be pre-selected; the browser
	// then shows the first option ("All") by default.
	res := getAnimalsPage(t, app, "/consolidated_animals")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	yearSelect := extractSelectByName(t, res.Body.String(), "year")
	assert.NotContains(t, yearSelect, "selected",
		"default year dropdown must not mark any option selected (All must be the default)")

	// Filtering by year=2024: exactly one option selected — 2024.
	res = getAnimalsPage(t, app, "/consolidated_animals?year=2024")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	yearSelect = extractSelectByName(t, res.Body.String(), "year")
	selectedCount := strings.Count(yearSelect, "selected")
	assert.Equal(t, 1, selectedCount,
		"exactly one year option must be selected when filtering, got %d in: %s", selectedCount, yearSelect)
	assert.Contains(t, yearSelect, `<option value="2024" selected`,
		"the filtered year (2024) must be the selected option: %s", yearSelect)
}

// extractSelectByName returns the full <select name="...">...</select> fragment
// so assertions are scoped to a single dropdown (several dropdowns on the page
// use `selected`).
func extractSelectByName(t *testing.T, body, name string) string {
	t.Helper()
	open := fmt.Sprintf(`<select name=%q`, name)
	start := strings.Index(body, open)
	require.NotEqual(t, -1, start, "select %q must be present", name)
	end := strings.Index(body[start:], "</select>")
	require.NotEqual(t, -1, end, "select %q must be closed", name)
	return body[start : start+end]
}
