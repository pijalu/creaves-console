//go:build sqlite
// +build sqlite

package actions

// Regression tests for bugs.md item 7 (MEDIUM console UX):
//  1. The register table jammed the status badge, outcome badge and outtake
//     type `<small>` together with no separator ("ReleasedNegative DCD").
//     Status/outcome badges must carry a margin class and the outtake type
//     must render as its own block-level line.
//  2. Animals whose stored outtake outcome is negative (DCD / euthanasia)
//     were labelled "Released" — legacy rows processed before outcome-aware
//     processing can carry current_status='released' while the outcome is
//     negative. The status badge must show the *effective* status: deceased
//     wins over released.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"creaves-console/models"

	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedDeceasedReleasedAnimal inserts one animal whose stored current_status
// says "released" but whose outtake outcome is negative (dead flag + negative
// rating) — the exact legacy row shape that produced the "ReleasedNegative
// DCD" jam. A unique ring makes the row extractable from the shared page.
func seedDeceasedReleasedAnimal(t *testing.T, tx *pop.Connection) {
	t.Helper()
	now := time.Now().UTC()
	a := &models.ConsolidatedAnimal{
		ID:            uuid.Must(uuid.NewV4()),
		InstanceID:    "center-a",
		AnimalID:      901,
		Year:          2024,
		YearNumber:    901,
		CurrentStatus: "released",
		LastEventAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	a.Ring = nulls.NewString("DCDROW-1")
	a.OuttakeType = nulls.NewString("DCD")
	a.OuttakeRating = nulls.NewInt(-2)
	a.OuttakeDead = nulls.NewBool(true)
	require.NoError(t, tx.Create(a))
}

// extractRowContaining returns the full <tr>...</tr> block that contains the
// given needle, so assertions can be scoped to a single table row.
func extractRowContaining(t *testing.T, body, needle string) string {
	t.Helper()
	idx := strings.Index(body, needle)
	require.NotEqual(t, -1, idx, "needle %q not found in body", needle)
	start := strings.LastIndex(body[:idx], "<tr>")
	require.NotEqual(t, -1, start, "no <tr> before needle %q", needle)
	end := strings.Index(body[idx:], "</tr>")
	require.NotEqual(t, -1, end, "no </tr> after needle %q", needle)
	return body[start : idx+end+len("</tr>")]
}

// TestRegisterBadgeSeparationAndDeceasedSemantics asserts on the register
// index page:
//   - the deceased-released legacy row shows the Died badge (danger), never a
//     Released (primary) badge;
//   - the status and outcome badges are visually separated (margin class);
//   - the outtake type renders as its own block-level muted line.
func TestRegisterBadgeSeparationAndDeceasedSemantics(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)
	seedDeceasedReleasedAnimal(t, testDB)

	res := getAnimalsPage(t, app, "/consolidated_animals")
	require.Equal(t, http.StatusOK, res.Code, "status: %d", res.Code)
	body := res.Body.String()

	row := extractRowContaining(t, body, "DCDROW-1")

	// Semantics: negative outcome forces the Died badge.
	assert.Contains(t, row, "badge-danger", "deceased animal must carry the danger status badge: %s", row)
	assert.Contains(t, row, "Died", "deceased animal status label must read Died: %s", row)
	assert.NotContains(t, row, "badge-primary", "deceased animal must NOT be labelled Released: %s", row)
	assert.NotContains(t, row, "Released", "deceased animal must NOT show a Released label: %s", row)

	// Outcome badge still shown next to the status badge.
	assert.Contains(t, row, "Negative", "outcome badge must render the negative outcome label: %s", row)

	// Visual separation: badges carry a margin class instead of being glued.
	assert.Contains(t, row, `badge badge-danger mr-1`, "status badge must carry the mr-1 separation margin: %s", row)
	assert.Regexp(t, `badge badge-\w+ mr-1">Negative`, row,
		"outcome badge must carry the mr-1 separation margin: %s", row)

	// Outtake type on its own line, not glued to the badges.
	assert.Regexp(t, `<small class="d-block text-muted">`, row,
		"outtake type must render as a separate muted block: %s", row)
}

// TestShowPageDeceasedSemantics asserts the animal detail page applies the
// same effective-status rule (deceased wins over released).
func TestShowPageDeceasedSemantics(t *testing.T) {
	app := newDashboardTestApp(testDB)
	seedRegisterFixtures(t, testDB)
	seedDeceasedReleasedAnimal(t, testDB)

	var id string
	require.NoError(t, testDB.RawQuery("SELECT id FROM consolidated_animals WHERE ring = ?", "DCDROW-1").First(&id))
	require.NotEmpty(t, id, "deceased fixture row must exist")

	res := getAnimalsPage(t, app, "/consolidated_animals/"+id)
	require.Equal(t, http.StatusOK, res.Code, "status: %d", res.Code)
	body := res.Body.String()

	assert.Contains(t, body, "Died", "detail page status label must read Died for a negative outcome: %s", body)
	assert.NotContains(t, body, ">Released<", "detail page must not label the animal Released: %s", body)
}

// TestEffectiveStatusUnit exercises the model-level rule directly: a negative
// outtake outcome (rating < 0 or dead flag) forces "died" regardless of the
// stored current_status.
func TestEffectiveStatusUnit(t *testing.T) {
	build := func(status string, rating nulls.Int, dead nulls.Bool) models.ConsolidatedAnimal {
		return models.ConsolidatedAnimal{
			CurrentStatus: status,
			OuttakeRating: rating,
			OuttakeDead:   dead,
		}
	}
	cases := []struct {
		name     string
		animal   models.ConsolidatedAnimal
		expected string
	}{
		{"legacy released with negative rating shows died",
			build("released", nulls.NewInt(-2), nulls.NewBool(false)), "died"},
		{"legacy released dead flag shows died",
			build("released", nulls.Int{}, nulls.NewBool(true)), "died"},
		{"genuinely released stays released",
			build("released", nulls.NewInt(2), nulls.NewBool(false)), "released"},
		{"neutral released outcome stays released",
			build("released", nulls.NewInt(0), nulls.NewBool(false)), "released"},
		{"in care untouched",
			build("in_care", nulls.Int{}, nulls.Bool{}), "in_care"},
		{"died stays died",
			build("died", nulls.Int{}, nulls.Bool{}), "died"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.animal.EffectiveStatus())
		})
	}
}
