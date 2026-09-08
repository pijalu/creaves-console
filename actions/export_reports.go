package actions

import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
)

// Handlers for the ported Creaves export reports (bugs.md item 3):
// online HTML view + CSV download, scoped to all instances (default) or a
// single instance through reportScope like every other console report.

// dfPlaceholderRe matches a {df:column:format} placeholder.
var dfPlaceholderRe = regexp.MustCompile(`\{df:([a-z_]+):([^}]+)\}`)

// colPlaceholderRe matches a {year:column} or {dow:column} placeholder.
var colPlaceholderRe = regexp.MustCompile(`\{(year|dow):([a-z_]+)\}`)

// buildExportSQL substitutes the placeholders of q for the given scope and
// dialect and returns the final SQL plus the scope argument (if any).
func buildExportSQL(q exportQuery, scope ReportScope, dialect string) (string, []interface{}) {
	sql := q.SQL

	// Date-format placeholders {df:col:fmt} -> dialect-specific expression.
	sql = dfPlaceholderRe.ReplaceAllStringFunc(sql, func(m string) string {
		parts := dfPlaceholderRe.FindStringSubmatch(m)
		col, format := parts[1], parts[2]
		return dateFormatExpr(dialect, "a."+col, format)
	})

	// Column placeholders {year:col} and {dow:col} -> dialect-specific
	// year / weekday-number extraction.
	sql = colPlaceholderRe.ReplaceAllStringFunc(sql, func(m string) string {
		parts := colPlaceholderRe.FindStringSubmatch(m)
		kind, col := parts[1], "a."+parts[2]
		if kind == "year" {
			return yearExpr(dialect, col)
		}
		return dowExpr(dialect, col)
	})

	// Days-in-care.
	sql = strings.ReplaceAll(sql, "{stay}", stayExprExport(dialect))

	// Boolean literal.
	sql = strings.ReplaceAll(sql, "{true}", boolLiteral(dialect))

	// Instance scope.
	var args []interface{}
	if scope.IsGlobal() {
		sql = strings.ReplaceAll(sql, "{scopeWhere}", "")
		sql = strings.ReplaceAll(sql, "{scopeAnd}", "")
	} else {
		sql = strings.ReplaceAll(sql, "{scopeWhere}", "WHERE a.instance_id = ?")
		sql = strings.ReplaceAll(sql, "{scopeAnd}", "AND a.instance_id = ?")
		args = []interface{}{scope.InstanceID}
	}
	return sql, args
}

// dateFormatExpr renders a dialect-specific date-formatting expression.
// fmt uses MySQL DATE_FORMAT tokens (%d/%m/%Y etc.); for SQLite they are
// translated to strftime tokens (identical for the tokens we use).
func dateFormatExpr(dialect, col, format string) string {
	if dialect == "sqlite" || dialect == "sqlite3" {
		return fmt.Sprintf("strftime('%s', %s)", sqliteDateFormat(format), col)
	}
	return fmt.Sprintf("DATE_FORMAT(%s, '%s')", col, format)
}

// sqliteDateFormat maps the MySQL DATE_FORMAT tokens used by the ported
// queries to strftime tokens. %d, %m, %Y, %H, %i are identical; only tokens
// that differ need mapping.
func sqliteDateFormat(format string) string {
	r := strings.NewReplacer(
		"%Y", "%Y", // year (4-digit)
		"%m", "%m", // month (2-digit)
		"%d", "%d", // day of month (2-digit)
		"%H", "%H", // hour (00-23)
		"%i", "%M", // minutes -> strftime %M
	)
	return r.Replace(format)
}

// stayExprExport is the dialect-specific "days in care" expression
// (outtake - intake + 1), empty when either date is missing.
func stayExprExport(dialect string) string {
	if dialect == "sqlite" || dialect == "sqlite3" {
		return "CAST(JULIANDAY(a.outtake_date) - JULIANDAY(a.intake_date) AS INTEGER) + 1"
	}
	return "DATEDIFF(a.outtake_date, a.intake_date) + 1"
}

// yearExpr renders a dialect-specific 4-digit year extraction.
func yearExpr(dialect, col string) string {
	if dialect == "sqlite" || dialect == "sqlite3" {
		return fmt.Sprintf("strftime('%%Y', %s)", col)
	}
	return fmt.Sprintf("YEAR(%s)", col)
}

// dowExpr renders a dialect-specific weekday-number extraction following the
// MySQL DAYOFWEEK convention (1 = Sunday .. 7 = Saturday). SQLite strftime
// '%w' returns 0 = Sunday, hence the +1.
func dowExpr(dialect, col string) string {
	if dialect == "sqlite" || dialect == "sqlite3" {
		return fmt.Sprintf("(CAST(strftime('%%w', %s) AS INTEGER) + 1)", col)
	}
	return fmt.Sprintf("DAYOFWEEK(%s)", col)
}

// boolLiteral renders a dialect-specific TRUE literal.
func boolLiteral(dialect string) string {
	if dialect == "sqlite" || dialect == "sqlite3" {
		return "1"
	}
	return "1" // MySQL accepts 1 for boolean comparison
}

// exportQueryer is satisfied by both pop stores that can run raw queries:
// *pop.dB (plain connection) and *pop.Tx (request transaction).
type exportQueryer interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}

// exportQueryStore extracts a raw query runner from a pop connection: the
// request transaction when set (popmw.Transaction), otherwise the store.
func exportQueryStore(tx *pop.Connection) (exportQueryer, error) {
	if tx.TX != nil && tx.TX.Tx != nil {
		return tx.TX, nil
	}
	if q, ok := tx.Store.(exportQueryer); ok {
		return q, nil
	}
	return nil, fmt.Errorf("export: pop connection exposes no raw query store")
}

// runExportQuery executes the report and returns columns + rows as strings
// (NULL rendered as "").
func runExportQuery(tx *pop.Connection, q exportQuery, scope ReportScope) ([]string, [][]string, error) {
	sqlStr, args := buildExportSQL(q, scope, tx.Dialect.Name())
	store, err := exportQueryStore(tx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := store.Query(sqlStr, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("error running export query %s: %w", q.Name, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, fmt.Errorf("error reading columns: %w", err)
	}

	rowPtr := make([]interface{}, len(cols))
	rowStr := make([]*string, len(cols))
	for i := range rowStr {
		rowPtr[i] = &rowStr[i]
	}

	var result [][]string
	for rows.Next() {
		if err := rows.Scan(rowPtr...); err != nil {
			return nil, nil, fmt.Errorf("error scanning row: %w", err)
		}
		rec := make([]string, len(cols))
		for i, s := range rowStr {
			if s != nil {
				rec[i] = *s
			}
		}
		result = append(result, rec)
	}
	return cols, result, rows.Err()
}

// exportReportContext resolves scope, finds the query, and runs it.
func exportReportContext(c buffalo.Context) (*exportQuery, ReportScope, []string, [][]string, error) {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return nil, ReportScope{}, nil, nil, fmt.Errorf("no transaction found")
	}
	scope, err := reportScope(c, tx)
	if err != nil {
		return nil, scope, nil, nil, err
	}
	q := findExportQuery(c.Param("query"))
	if q == nil {
		return nil, scope, nil, nil, c.Error(http.StatusNotFound, fmt.Errorf("unknown export query: %s", c.Param("query")))
	}
	cols, rows, err := runExportQuery(tx, *q, scope)
	if err != nil {
		return nil, scope, nil, nil, err
	}
	return q, scope, cols, rows, nil
}

// ExportReportsIndex lists all ported reports with links to the online view
// and CSV download, honoring the instance scope.
func ExportReportsIndex(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}
	scope, err := reportScope(c, tx)
	if err != nil {
		return err
	}
	instances, err := exportInstanceOptions(tx, scope)
	if err != nil {
		return err
	}
	c.Set("queries", exportQueries)
	c.Set("instances", instances)
	c.Set("instanceID", scope.InstanceID)
	return c.Render(http.StatusOK, r.HTML("export/index.plush.html"))
}

// ExportReportView renders the online (sortable/filterable) HTML view.
func ExportReportView(c buffalo.Context) error {
	q, scope, cols, rows, err := exportReportContext(c)
	if err != nil {
		return err
	}
	if q == nil {
		return c.Error(http.StatusNotFound, fmt.Errorf("unknown export query"))
	}
	instances, err := exportInstanceOptionsFromCtx(c, scope)
	if err != nil {
		return err
	}
	c.Set("query", q)
	c.Set("cols", cols)
	c.Set("rows", rows)
	c.Set("instances", instances)
	c.Set("instanceID", scope.InstanceID)
	c.Set("aggregate", q.Aggregate)
	return c.Render(http.StatusOK, r.HTML("export/view.plush.html"))
}

// ExportReportCSV streams the report as a CSV download.
func ExportReportCSV(c buffalo.Context) error {
	q, scope, cols, rows, err := exportReportContext(c)
	if err != nil {
		return err
	}
	if q == nil {
		return c.Error(http.StatusNotFound, fmt.Errorf("unknown export query"))
	}
	filename := fmt.Sprintf("%s.csv", q.Name)
	if !scope.IsGlobal() {
		filename = fmt.Sprintf("%s-%s.csv", q.Name, scope.InstanceID)
	}
	return writeCSV(c, filename, cols, rows)
}

// exportInstanceOptions builds the Center selector entries for the export
// pages (same pattern as the CSV reports index).
func exportInstanceOptions(tx *pop.Connection, scope ReportScope) ([]annualInstanceOption, error) {
	var rows []struct {
		InstanceID string `db:"instance_id"`
		Name       string `db:"name"`
	}
	if err := tx.RawQuery("SELECT instance_id, name FROM creaves_instances ORDER BY instance_id asc").All(&rows); err != nil {
		return nil, err
	}
	instances := []annualInstanceOption{{InstanceID: "", Name: "", Selected: scope.IsGlobal()}}
	for _, row := range rows {
		instances = append(instances, annualInstanceOption{
			InstanceID: row.InstanceID,
			Name:       row.Name,
			Selected:   scope.InstanceID == row.InstanceID,
		})
	}
	return instances, nil
}

func exportInstanceOptionsFromCtx(c buffalo.Context, scope ReportScope) ([]annualInstanceOption, error) {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return nil, fmt.Errorf("no transaction found")
	}
	return exportInstanceOptions(tx, scope)
}
