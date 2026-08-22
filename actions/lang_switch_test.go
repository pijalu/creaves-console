//go:build sqlite
// +build sqlite

package actions

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/mw-i18n/v2"
	"github.com/stretchr/testify/require"

	"creaves-console/locales"
	"creaves-console/templates"
)

// newLangTestApp mirrors the real App() wiring for the /lang group: it runs
// the i18n middleware (so the lang cookie drives template/translation
// selection) and mounts the language switcher routes without auth.
func newLangTestApp() *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	tr, err := i18n.New(locales.FS(), "en-US")
	if err != nil {
		panic(err)
	}
	// Mirrors translations() in App(): the package-level translator is used
	// by setLang via T.Refresh.
	T = tr
	app.Use(tr.Middleware())
	lang := app.Group("/lang")
	lang.GET("/", SwitchLanguage)
	lang.POST("/", SwitchLanguagePost)
	app.GET("/dashboard", func(c buffalo.Context) error {
		c.Set("stats", map[string]interface{}{
			"total_animals":       1,
			"total_events":        1,
			"unique_instances":    1,
			"active_webhook_keys": 1,
			"by_status":           map[string]interface{}{"in_care": 1},
			"by_instance":         map[string]interface{}{"a": 1},
		})
		return c.Render(http.StatusOK, r.HTML("dashboard/index.plush.html"))
	})
	return app
}

func TestRenderFr_VariantsExistForAllTemplates(t *testing.T) {
	err := fs.WalkDir(locales.FS(), ".", func(path string, d fs.DirEntry, err error) error { return err })
	require.NoError(t, err)
	// Verify embedded template tree directly through render's configured FS.
	frCount := 0
	err = fs.WalkDir(templates.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".plush.html") || strings.HasSuffix(path, ".plush.fr.html") {
			return nil
		}
		frCount++
		_, err = fs.Stat(templates.FS(), strings.TrimSuffix(path, ".plush.html")+".plush.fr.html")
		return err
	})
	require.NoError(t, err)
	require.Greater(t, frCount, 0)
}

func TestLangSwitcher_RendersFrenchVariant(t *testing.T) {
	app := newLangTestApp()
	req := httptest.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "lang", Value: "fr"})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `<html lang="fr">`)
	require.Contains(t, body, "Tableau de bord")
	require.Contains(t, body, "/lang/?lang=en-US")
	require.Contains(t, body, "/lang/?lang=de")
	require.Contains(t, body, "/lang/?lang=nl")
}

func TestLangSwitcher_RendersGermanVariant(t *testing.T) {
	app := newLangTestApp()
	req := httptest.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "lang", Value: "de"})
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Tiere gesamt")
	require.Contains(t, body, "Konsolidierte Ansicht")
	require.Contains(t, body, "/lang/?lang=en-US")
	require.Contains(t, body, "/lang/?lang=nl")
}

func TestLangSwitcher_RendersEnglishByDefault(t *testing.T) {
	app := newLangTestApp()
	req := httptest.NewRequest("GET", "/dashboard", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "Total Animals")
	require.Contains(t, body, "/lang/?lang=de")
	require.Contains(t, body, "/lang/?lang=nl")
}

func TestLangSwitcher_GETSetsCookieAndRedirects(t *testing.T) {
	app := newLangTestApp()
	req := httptest.NewRequest("GET", "/lang/?lang=de&url=%2Fdashboard", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, "/dashboard", rec.Header().Get("Location"))
	cookies := rec.Result().Cookies()
	var lang *http.Cookie
	for _, c := range cookies {
		if c.Name == "lang" {
			lang = c
		}
	}
	require.NotNil(t, lang, "lang cookie must be set")
	require.Equal(t, "de", lang.Value)
}

func TestLangSwitcher_POSTSetsCookieAndRedirects(t *testing.T) {
	app := newLangTestApp()
	req := httptest.NewRequest("POST", "/lang/", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()
	req.Form.Set("lang", "nl")
	req.Form.Set("url", "/reports")
	req = httptest.NewRequest("POST", "/lang/", nil)
	body := "lang=nl&url=%2Freports"
	req = httptest.NewRequest("POST", "/lang/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, "/reports", rec.Header().Get("Location"))
	cookies := rec.Result().Cookies()
	var lang *http.Cookie
	for _, c := range cookies {
		if c.Name == "lang" {
			lang = c
		}
	}
	require.NotNil(t, lang)
	require.Equal(t, "nl", lang.Value)
}
