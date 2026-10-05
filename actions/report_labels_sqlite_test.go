package actions

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"creaves-console/models"
)

// Round 10 performance pins (docs/performance-assessment-2026-10-04.md).

// newReportTestApp serves the by-species report over the given tx with a
// fixed UI language (the labels path under test).
func newReportTestApp(tx *pop.Connection, lang string) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			if lang != "" {
				c.Request().AddCookie(&http.Cookie{Name: "lang", Value: lang})
			}
			return next(c)
		}
	})
	app.GET("/reports/by_species", ReportsBySpecies)
	return app
}

// seedLabelFixtures: two species — Hérisson rows all carry the same
// translations JSON with an en-US species label, Corneille rows have none.
func seedLabelFixtures(t *testing.T, tx *pop.Connection) {
	t.Helper()
	require.NoError(t, tx.RawQuery("DELETE FROM consolidated_animals").Exec())

	base := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	rows := []struct {
		number       int
		species      string
		translations string
	}{
		// Production invariant: rows sharing a canonical value share the
		// same translation set (keyed by the creaves reference record) —
		// the representative-row fetch relies on it. Corneille has none
		// (legacy rows: canonical fallback).
		{1, "Hérisson", `{"en-US": {"species": "West European Hedgehog"}}`},
		{2, "Hérisson", `{"en-US": {"species": "West European Hedgehog"}}`},
		{3, "Hérisson", `{"en-US": {"species": "West European Hedgehog"}}`},
		{4, "Corneille noire", ""},
	}
	for _, r := range rows {
		a := &models.ConsolidatedAnimal{
			ID:            uuid.Must(uuid.NewV4()),
			InstanceID:    "center-a",
			AnimalID:      100 + r.number,
			Year:          2026,
			YearNumber:    r.number,
			Species:       nulls.NewString(r.species),
			CurrentStatus: "in_care",
			IntakeDate:    nulls.NewTime(base),
			LastEventAt:   base,
		}
		if r.translations != "" {
			a.Translations = nulls.NewString(r.translations)
		}
		require.NoError(t, tx.Create(a))
	}
}

// R10-P5: report group labels must resolve WITHOUT aggregating over the
// translations JSON column (MIN(translations) measured ~34ms per report
// page on 12k rows). The optimized two-step must return the same labels
// as before: en-US translated when a translation exists, canonical
// otherwise; fr/base returns canonical values with the JSON never read.
func TestReportGroupLabelsTwoStep(t *testing.T) {
	seedLabelFixtures(t, testDB)

	// Direct call: en-US — translated where available, canonical fallback.
	en, err := localizedGroupLabels(testDB, ReportScope{}, "species", "en-US", "WHERE species IS NOT NULL", nil)
	require.NoError(t, err)
	assert.Equal(t, "West European Hedgehog", en["Hérisson"], "translated label must survive the two-step")
	assert.Equal(t, "Corneille noire", en["Corneille noire"], "canonical fallback must survive")

	// Direct call: base/fr fast path — canonical without touching JSON.
	fr, err := localizedGroupLabels(testDB, ReportScope{}, "species", "", "WHERE species IS NOT NULL", nil)
	require.NoError(t, err)
	assert.Equal(t, "Hérisson", fr["Hérisson"])
	assert.Equal(t, "Corneille noire", fr["Corneille noire"])
	assert.Len(t, fr, 2)
}

// The full by-species page must render the same labels through the
// optimized labels path in both language modes.
func TestReportsBySpeciesRendersLabels(t *testing.T) {
	seedLabelFixtures(t, testDB)

	for _, tc := range []struct{ lang, want string }{
		{"en-US", "West European Hedgehog"},
		{"fr", "Hérisson"},
	} {
		app := newReportTestApp(testDB, tc.lang)
		req, err := http.NewRequest("GET", "/reports/by_species", nil)
		require.NoError(t, err)
		res := httptest.NewRecorder()
		app.ServeHTTP(res, req)
		require.Equal(t, http.StatusOK, res.Code, tc.lang)
		assert.Contains(t, res.Body.String(), tc.want,
			"%s page must render the %q label", tc.lang, tc.want)
	}
}
