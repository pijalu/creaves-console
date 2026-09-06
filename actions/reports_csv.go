package actions

import (
	"github.com/gobuffalo/buffalo"
)

// ReportsCSVIndex renders the dedicated CSV reports section: one card per
// exportable report (consolidated animals, yearly register, register
// snapshot, annual report), each with a link to the online view (sortable /
// filterable in the browser) and a direct CSV download link.
//
// Downloads and online views honor the instance scope on their own pages:
// the links below carry no instance_id parameter so each report opens with
// its default "all centers" scope; the user narrows to a given instance via
// the Center selector of the target page.
func ReportsCSVIndex(c buffalo.Context) error {
	return c.Render(200, r.HTML("reports/csv.plush.html"))
}
