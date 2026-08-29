//go:build sqlite
// +build sqlite

package actions

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReportsAnnualIndex_TemplateVariants(t *testing.T) {
	seedAnnualFixtures(t, testDB)
	app := newAnnualTestApp(testDB)
	for _, lang := range []string{"en-US", "fr", "de", "nl"} {
		res := getAnnual(t, app, "/reports/annual?year=2024", lang)
		require.Equal(t, http.StatusOK, res.Code, "lang %s body: %s", lang, res.Body.String())
		require.Contains(t, res.Body.String(), "Hérisson", "lang %s", lang)
	}
}
