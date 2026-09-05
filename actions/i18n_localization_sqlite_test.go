//go:build sqlite
// +build sqlite

package actions

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"creaves-console/models"
	"creaves-console/locales"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/mw-i18n/v2"
	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin bug 6 (console side): term values (species, type, age,
// entry cause, outtake type) must render localized from the stored payload
// translations in dropdowns, report tables and CSV exports, while select
// options keep the canonical value for filtering.

const i18nTranslationsFixture = `{"nl":{"species":"Egel","animal_type":"Zoogdier","animal_age":"Volwassen","entry_cause":"Aanrijding","outtake_type":"Vrijlating"},"de":{"species":"Igel","animal_type":"Säugetier"}}`

// seedI18nAnimal resets the register tables and inserts one instance with a
// single animal carrying canonical French values plus nl/de translations.
func seedI18nAnimal(t *testing.T, tx *pop.Connection) {
	t.Helper()
	require.NoError(t, tx.RawQuery("DELETE FROM consolidated_animals").Exec())
	require.NoError(t, tx.RawQuery("DELETE FROM creaves_instances").Exec())

	now := time.Now().UTC()
	inst := &models.CreavesInstance{ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", Name: "Center A", FirstSeenAt: now, LastSeenAt: now}
	require.NoError(t, tx.Create(inst))

	intake := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
	outtake := time.Date(2024, 6, 1, 9, 0, 0, 0, time.UTC)
	a := &models.ConsolidatedAnimal{
		ID:            uuid.Must(uuid.NewV4()),
		InstanceID:    "center-a",
		AnimalID:      1,
		Year:          2024,
		YearNumber:    1,
		CurrentStatus: "released",
		LastEventAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
		IntakeDate:    nulls.NewTime(intake),
		OuttakeDate:   nulls.NewTime(outtake),
	}
	a.Species = nulls.NewString("Hérisson")
	a.AnimalType = nulls.NewString("Mammifère")
	a.AnimalAge = nulls.NewString("Adulte")
	a.EntryCause = nulls.NewString("Collision")
	a.OuttakeType = nulls.NewString("Relâché")
	a.Translations = nulls.NewString(i18nTranslationsFixture)
	require.NoError(t, tx.Create(a))
}

// newI18nTestApp builds an app with tx + i18n middleware (lang cookie drives
// template variant selection) mounting the console pages under test.
func newI18nTestApp(tx *pop.Connection) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	tr, err := i18n.New(locales.FS(), "en-US")
	if err != nil {
		panic(err)
	}
	T = tr
	app.Use(tr.Middleware())
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/consolidated_animals", ConsolidatedAnimalsIndex)
	app.GET("/consolidated_animals/export.csv", ConsolidatedAnimalsExportCSV)
	app.GET("/reports/register", RegisterIndex)
	app.GET("/reports/register/export.csv", RegisterExportCSV)
	app.GET("/reports/snapshot/export.csv", SnapshotExportCSV)
	return app
}

func i18nGet(t *testing.T, app *buffalo.App, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: "lang", Value: "nl"})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func csvRows(t *testing.T, rec *httptest.ResponseRecorder) [][]string {
	t.Helper()
	parsed, err := csv.NewReader(rec.Body).ReadAll()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(parsed), 2)
	return parsed
}

func TestI18n_Index_NL_LocalizedDropdowns(t *testing.T) {
	tx := testDB
	seedI18nAnimal(t, tx)
	app := newI18nTestApp(tx)

	// view=detailed: the compact default layout has no entry_cause cell.
	rec := i18nGet(t, app, "/consolidated_animals?view=detailed")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	// Option values stay canonical (filter params match stored columns)…
	assert.Contains(t, body, `value="Hérisson"`)
	assert.Contains(t, body, `value="Collision"`)
	// …while the rendered labels come from the stored translations.
	assert.Contains(t, body, ">Egel</option>")
	assert.Contains(t, body, ">Zoogdier</option>")
	assert.Contains(t, body, ">Volwassen</option>")
	assert.Contains(t, body, ">Aanrijding</option>")
	assert.Contains(t, body, ">Vrijlating</option>")
	// The register table row renders the localized labels; anchor on the
	// closing cell tag so dropdown options (which legitimately end in
	// </option>) cannot satisfy the assertion — template variants have
	// drifted before, leaving canonical French in the fr/de/nl tables.
	assert.Regexp(t, `Egel\s*</td>`, body)
	assert.NotRegexp(t, `Hérisson\s*</td>`, body)
	// entry_cause table cell must localize as well.
	assert.Regexp(t, `Aanrijding\s*</td>`, body)
	assert.NotRegexp(t, `Collision\s*</td>`, body)
}

func TestI18n_Register_NL_LocalizedCells(t *testing.T) {
	tx := testDB
	seedI18nAnimal(t, tx)
	app := newI18nTestApp(tx)

	rec := i18nGet(t, app, "/reports/register?year=2024")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Contains(t, body, ">Zoogdier<")
	assert.Contains(t, body, ">Egel<")
	assert.Contains(t, body, ">Volwassen<")
	assert.Contains(t, body, ">Aanrijding<")
	assert.Contains(t, body, ">Vrijlating<")
	assert.NotContains(t, body, ">Hérisson<")
}

func TestI18n_Register_ExportCSV_NL_LocalizedValues(t *testing.T) {
	tx := testDB
	seedI18nAnimal(t, tx)
	app := newI18nTestApp(tx)

	rec := i18nGet(t, app, "/reports/register/export.csv?year=2024")
	require.Equal(t, http.StatusOK, rec.Code)
	rows := csvRows(t, rec)
	joined := strings.Join(rows[1], ";")
	assert.Contains(t, joined, "Egel")
	assert.Contains(t, joined, "Zoogdier")
	assert.Contains(t, joined, "Volwassen")
	assert.Contains(t, joined, "Aanrijding")
	assert.Contains(t, joined, "Vrijlating")
	assert.NotContains(t, joined, "Hérisson")
}

func TestI18n_Snapshot_ExportCSV_NL_LocalizedValues(t *testing.T) {
	tx := testDB
	seedI18nAnimal(t, tx)
	app := newI18nTestApp(tx)

	// Snapshot while the animal was still in care (outtake on 2024-06-01).
	rec := i18nGet(t, app, "/reports/snapshot/export.csv?snapshotDate=2024-03-01")
	require.Equal(t, http.StatusOK, rec.Code)
	rows := csvRows(t, rec)
	joined := strings.Join(rows[1], ";")
	assert.Contains(t, joined, "Egel")
	assert.Contains(t, joined, "Zoogdier")
	assert.Contains(t, joined, "Volwassen")
	assert.Contains(t, joined, "Aanrijding")
}

func TestI18n_Consolidated_ExportCSV_NL_LocalizedValues(t *testing.T) {
	tx := testDB
	seedI18nAnimal(t, tx)
	app := newI18nTestApp(tx)

	rec := i18nGet(t, app, "/consolidated_animals/export.csv")
	require.Equal(t, http.StatusOK, rec.Code)
	rows := csvRows(t, rec)
	joined := strings.Join(rows[1], ";")
	assert.Contains(t, joined, "Egel")
	assert.Contains(t, joined, "Zoogdier")
	assert.Contains(t, joined, "Volwassen")
	assert.Contains(t, joined, "Aanrijding")
	assert.Contains(t, joined, "Vrijlating")
	assert.NotContains(t, joined, "Hérisson")
}
