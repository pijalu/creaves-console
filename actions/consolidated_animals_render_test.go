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
// don't. Those columns belong to the *detailed* layout, so the test requests
// view=detailed explicitly (compact is the default, bugs.md 'consolidated view').
func TestConsolidatedAnimalsRegisterRendersNonEmptyCells(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	res := getAnimalsPage(t, app, "/consolidated_animals?view=detailed")
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

// TestConsolidatedAnimalsDefaultViewIsCompact asserts the register defaults to
// the compact 7-column layout (bugs.md 'consolidated view'): Instance, Year,
// N°, Species, Intake date, Outcome date, Outcome — and none of the
// detailed-only columns.
func TestConsolidatedAnimalsDefaultViewIsCompact(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)
	seedDeceasedReleasedAnimal(t, testDB)

	res := getAnimalsPage(t, app, "/consolidated_animals")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	body := res.Body.String()

	thead := extractThead(t, body)
	assert.Equal(t, 7, strings.Count(thead, "<th>"),
		"compact view must render exactly 7 columns, got: %s", thead)

	// Compact column set present (sortable headers carry sort=<field>).
	for _, field := range []string{"instance", "year", "number", "species", "intake_date", "outcome_date", "outcome"} {
		assert.Contains(t, thead, "sort="+field, "compact header must contain sort=%s", field)
	}

	// Detailed-only columns absent from the header.
	for _, field := range []string{"identification", "discovery_location", "postal_code", "entry_cause", "outcome_location"} {
		assert.NotContains(t, thead, "sort="+field, "compact header must NOT contain sort=%s", field)
	}
	assert.NotContains(t, thead, "Actions", "compact view must not render the Actions column")

	// The deceased fixture row (rating -2 + dead flag) shows the negative
	// outcome badge in the compact Outcome column.
	assert.Contains(t, body, `badge badge-danger">Negative`,
		"compact view must render the negative outcome badge")

	// The compact toggle must be active and the detailed toggle offered.
	assert.Contains(t, body, `id="view-compact"`, "view toggle must offer the compact layout")
	assert.Contains(t, body, `id="view-detailed"`, "view toggle must offer the detailed layout")
}

// TestConsolidatedAnimalsDetailedTogglePreservesFilters asserts the detailed
// toggle link carries view=detailed plus the active filters, and that the
// detailed layout restores the full column set.
func TestConsolidatedAnimalsDetailedTogglePreservesFilters(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	res := getAnimalsPage(t, app, "/consolidated_animals?year=2024&species=H%C3%A9risson")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	body := res.Body.String()

	thead := extractThead(t, body)
	assert.Equal(t, 7, strings.Count(thead, "<th>"), "default layout stays compact under filters")

	// The detailed toggle preserves the active filters.
	toggle := extractAnchorByID(t, body, "view-detailed")
	assert.Contains(t, toggle, "view=detailed", "detailed toggle must link to view=detailed: %s", toggle)
	assert.Contains(t, toggle, "year=2024", "detailed toggle must preserve the year filter: %s", toggle)
	assert.Contains(t, toggle, "species=", "detailed toggle must preserve the species filter: %s", toggle)

	// Switching to detailed restores the full column set (and keeps filters).
	res = getAnimalsPage(t, app, "/consolidated_animals?year=2024&species=H%C3%A9risson&view=detailed")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	thead = extractThead(t, res.Body.String())
	assert.Equal(t, 15, strings.Count(thead, "<th>"),
		"detailed view must render the full 15-column set, got: %s", thead)
	assert.Contains(t, thead, "sort=identification", "detailed header must contain the Identification column")

	// The compact toggle preserves the active filters too (and drops
	// view=detailed — compact is the default).
	body = res.Body.String()
	toggle = extractAnchorByID(t, body, "view-compact")
	assert.Contains(t, toggle, "year=2024", "compact toggle must preserve the year filter: %s", toggle)
	assert.NotContains(t, toggle, "view=detailed", "compact toggle must not keep view=detailed: %s", toggle)
}

// extractAnchorByID returns the full <a ...> tag carrying the given id, so
// assertions are scoped to a single link regardless of attribute order.
func extractAnchorByID(t *testing.T, body, id string) string {
	t.Helper()
	idx := strings.Index(body, `id="`+id+`"`)
	require.NotEqual(t, -1, idx, "anchor with id %q must be present", id)
	start := strings.LastIndex(body[:idx], "<a ")
	require.NotEqual(t, -1, start, "no <a> before id %q", id)
	end := strings.Index(body[idx:], ">")
	require.NotEqual(t, -1, end, "anchor %q must be closed", id)
	return body[start : idx+end+1]
}

// extractThead returns the first <thead>...</thead> fragment so header
// assertions are scoped to the register table's column set.
func extractThead(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, "<thead")
	require.NotEqual(t, -1, start, "thead must be present")
	end := strings.Index(body[start:], "</thead>")
	require.NotEqual(t, -1, end, "thead must be closed")
	return body[start : start+end]
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
