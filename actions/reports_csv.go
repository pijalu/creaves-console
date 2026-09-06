package actions

import (
	"fmt"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
)

// ReportsCSVIndex renders the dedicated CSV/Excel reports section: one card
// per exportable report (consolidated animals, yearly register, register
// snapshot, annual report), each with a link to the online view (sortable /
// filterable in the browser) and a direct CSV download link, plus the Excel
// register exports (registre_detail, stat_communes — Bug 6).
//
// Downloads and online views honor the instance scope on their own pages:
// the CSV links below carry no instance_id parameter so each report opens
// with its default "all centers" scope; the user narrows to a given instance
// via the Center selector of the target page. The Excel exports have no
// online view, so this page carries its own Center selector and the Excel
// links pass the selected instance_id straight to /export/excel.
func ReportsCSVIndex(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}
	scope, err := reportScope(c, tx)
	if err != nil {
		return err
	}
	// Raw query on the two needed columns only: creaves_instances.description
	// is NULLable in dev data while the model maps it to a plain string, so
	// tx.All(&models.CreavesInstances{}) would fail scanning.
	var rows []struct {
		InstanceID string `db:"instance_id"`
		Name       string `db:"name"`
	}
	if err := tx.RawQuery("SELECT instance_id, name FROM creaves_instances ORDER BY instance_id asc").All(&rows); err != nil {
		return err
	}
	instances := []annualInstanceOption{{InstanceID: "", Name: "", Selected: scope.IsGlobal()}}
	for _, row := range rows {
		instances = append(instances, annualInstanceOption{
			InstanceID: row.InstanceID,
			Name:       row.Name,
			Selected:   scope.InstanceID == row.InstanceID,
		})
	}
	c.Set("instances", instances)
	c.Set("instanceID", scope.InstanceID)
	return c.Render(200, r.HTML("reports/csv.plush.html"))
}
