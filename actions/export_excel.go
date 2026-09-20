package actions

import (
	"fmt"

	"creaves-console/excel"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
)

// ExportExcel handles GET
// /export/excel?query=registre_detail|stat_communes&instance_id=…&year=…
//
// Excel variant of the consolidated reports (Bug 6): the Creaves register
// exports running against consolidated_animals, for all instances (no
// instance_id) or a single instance, optionally restricted to one year
// (bug 4). The scope is resolved through reportScope like every other
// console report (404 on unknown instance); excel.RunQuery applies the
// instance and year predicates itself.
func ExportExcel(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}
	scope, err := reportScope(c, tx)
	if err != nil {
		return err
	}
	return excel.RunQuery(c, tx, c.Param("query"), scope.InstanceID, parseExportYear(c))
}
