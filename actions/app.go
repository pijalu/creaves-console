package actions

import (
	"net/http"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/buffalo-pop/v3/pop/popmw"
	"github.com/gobuffalo/envy"
	"github.com/gobuffalo/mw-csrf"
	"github.com/gobuffalo/mw-i18n/v2"
	"github.com/gobuffalo/mw-paramlogger"

	"creaves-console/models"
	"creaves-console/public"
	"creaves-console/locales"
)

var ENV = envy.Get("GO_ENV", "development")
var app *buffalo.App
var T *i18n.Translator

func App() *buffalo.App {
	if app == nil {
		port := envy.Get("PORT", "3001")
		app = buffalo.New(buffalo.Options{
			Env:         ENV,
			SessionName: "_creaves_console_session",
			Addr:        "127.0.0.1:" + port,
		})

		app.Use(paramlogger.ParameterLogger)
		app.Use(csrf.New)
		app.Use(popmw.Transaction(models.DB))
		app.Use(translations())

		// Webhook receiver - no session auth required, skip CSRF
		wh := app.Group("/webhook")
		wh.Middleware.Skip(csrf.New, WebhookEventsHandler)
		wh.POST("/events", WebhookEventsHandler)

		app.GET("/", DashboardIndex)

		app.Use(SetCurrentUser)
		app.Use(Authorize)

		// Auth routes
		auth := app.Group("/auth")
		auth.GET("/", AuthLanding)
		auth.GET("/new", AuthNew)
		auth.POST("/", AuthCreate)
		auth.DELETE("/", AuthDestroy)
		auth.Middleware.Skip(Authorize, AuthLanding, AuthNew, AuthCreate)

		// User management
		app.Resource("/users", UsersResource{})

		// Webhook API keys management
		app.Resource("/webhook_api_keys", WebhookAPIKeysResource{})

		// Consolidated animals
		app.GET("/consolidated_animals", ConsolidatedAnimalsIndex)
		app.GET("/consolidated_animals/:consolidated_animal_id", ConsolidatedAnimalShow)
		app.GET("/consolidated_animals/:consolidated_animal_id/drill_down", ConsolidatedAnimalDrillDown)

		// Dashboard
		app.GET("/dashboard", DashboardIndex)

		// Reports
		app.GET("/reports", ReportsIndex)
		app.GET("/reports/by_location", ReportsByLocation)
		app.GET("/reports/by_type", ReportsByType)
		app.GET("/reports/by_species", ReportsBySpecies)

		if ENV != "development" {
			app.ErrorHandlers[500] = func(status int, err error, c buffalo.Context) error {
				c.Flash().Add("danger", err.Error())
				return c.Render(status, r.HTML("/oops.plush.html"))
			}
		}

		app.ServeFiles("/", http.FS(public.FS()))
	}

	return app
}

func translations() buffalo.MiddlewareFunc {
	var err error
	if T, err = i18n.New(locales.FS(), "en-US"); err != nil {
		app.Stop(err)
	}
	return T.Middleware()
}
