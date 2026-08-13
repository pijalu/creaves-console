package actions

import (
	"creaves-console/public"
	"creaves-console/templates"

	"github.com/gobuffalo/buffalo/render"
)

var r *render.Engine

func init() {
	r = render.New(render.Options{
		HTMLLayout:     "application.plush.html",
		TemplatesFS:    templates.FS(),
		AssetsFS:       public.FS(),
		Helpers: render.Helpers{
			"bool2html": func(s bool) string {
				if s {
					return "✓"
				}
				return "✗"
			},
		},
	})
}
