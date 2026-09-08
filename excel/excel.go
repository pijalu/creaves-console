// Package excel generates the console Excel exports (Bug 6): the Creaves
// register exports (registre_detail, stat_communes) running against the
// denormalized consolidated_animals table, for all instances (global scope)
// or a single instance.
//
// It is a port of creaves/excel (projects share no code) including the Bug 5
// pivot-cache fix helpers from day one: generated files rewrite the pivot
// cache range to the rows actually written and set refreshOnLoad="1" so
// Excel rebuilds the pivot tables on open instead of prompting a repair.
//
// Queries live in config/config.yaml with identical French column aliases to
// the Creaves originals (templates' pivot tables are bound to those
// headers). Instance scoping is applied through the {scopeWhere}/{scopeAnd}
// placeholders substituted by RunQuery; the instance id is always passed as
// a query parameter, never interpolated into the SQL.
package excel

import (
	"bytes"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/xuri/excelize/v2"
	"gopkg.in/yaml.v2"
)

//go:embed config/*
var excelConfig embed.FS

// Queries is one configured export query.
type Queries struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Template    string `yaml:"template"`
	Sheet       string `yaml:"sheet"`
	Query       string `yaml:"query"`
}

// Config is the parsed config/config.yaml.
type Config struct {
	Queries []Queries `yaml:"queries"`
}

var config *Config = getConfig()

func getConfig() *Config {
	var config Config

	configYamlData, err := excelConfig.ReadFile("config/config.yaml")
	if err != nil {
		panic(fmt.Errorf("failed to load excel configuration: %v", err))
	}

	if err := yaml.Unmarshal(configYamlData, &config); err != nil {
		panic(fmt.Sprintf("error decoding excel configuration file: %v", err))
	}

	return &config
}

func (c *Config) getQuery(id string) (*Queries, error) {
	for _, q := range c.Queries {
		if strings.EqualFold(q.Name, id) {
			return &q, nil
		}
	}
	return nil, fmt.Errorf("could not find query %s", id)
}

// GetQueries returns the configured export queries.
func GetQueries() []Queries {
	return config.Queries
}

// sheetPosition converts a 1-based (line, col) pair into an A1-style cell
// reference (e.g. line 3, col 28 -> AB3).
func sheetPosition(line, col int) string {
	result := ""

	for col > 0 {
		mod := (col - 1) % 26
		result = string(rune('A'+mod)) + result
		col = (col - 1) / 26
	}

	return fmt.Sprintf("%s%d", result, line)
}

// scopeSQL substitutes the {scopeWhere}/{scopeAnd} placeholders of a
// configured query for the given instance scope and returns the final SQL
// plus the query arguments. An empty instanceID selects the global scope
// (no predicate, no arguments).
func scopeSQL(query, instanceID string) (string, []interface{}) {
	if instanceID == "" {
		sql := strings.ReplaceAll(query, "{scopeWhere}", "")
		sql = strings.ReplaceAll(sql, "{scopeAnd}", "")
		return sql, nil
	}
	sql := strings.ReplaceAll(query, "{scopeWhere}", "WHERE a.instance_id = ?")
	sql = strings.ReplaceAll(sql, "{scopeAnd}", "AND a.instance_id = ?")
	return sql, []interface{}{instanceID}
}

// stayExpr returns the dialect-specific "days in care" expression (outtake
// - intake + 1): MySQL DATEDIFF in production, JULIANDAY arithmetic under
// SQLite (tests).
func stayExpr(dialect string) string {
	if dialect == "sqlite" || dialect == "sqlite3" {
		return "CAST(JULIANDAY(a.outtake_date) - JULIANDAY(a.intake_date) AS INTEGER) + 1"
	}
	return "DATEDIFF(a.outtake_date, a.intake_date) + 1"
}

// queryer is satisfied by both pop stores that can run raw queries:
// *pop.dB (plain connection, embeds *sqlx.DB) and *pop.Tx (request
// transaction, embeds *sqlx.Tx).
type queryer interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}

// queryStore extracts a raw query runner from a pop connection: the request
// transaction when set (popmw.Transaction), otherwise the connection store.
func queryStore(tx *pop.Connection) (queryer, error) {
	if tx.TX != nil && tx.TX.Tx != nil {
		return tx.TX, nil
	}
	if q, ok := tx.Store.(queryer); ok {
		return q, nil
	}
	return nil, fmt.Errorf("excel: pop connection exposes no raw query store")
}

// RunQuery executes the named export query against tx and writes the
// resulting XLSX file (built on the embedded template, pivot caches fixed
// up) to the response. instanceID scopes the export to one instance; an
// empty instanceID exports all instances.
func RunQuery(c buffalo.Context, tx *pop.Connection, query, instanceID string) error {
	sqlQuery, err := config.getQuery(query)
	if err != nil {
		c.Logger().Debugf("Could not find query %s", query)
		c.Response().WriteHeader(http.StatusNotFound)
		c.Response().Write([]byte("404 - Not Found"))
		return nil
	}

	file, err := excelConfig.Open("config/" + sqlQuery.Template)
	if err != nil {
		return fmt.Errorf("error opening template file: %v", err)
	}
	defer file.Close()

	f, err := excelize.OpenReader(file)
	if err != nil {
		return fmt.Errorf("error opening template: %v", err)
	}
	defer f.Close()

	// Run the SQL query against the database and return the result set.
	sqlStr, args := scopeSQL(sqlQuery.Query, instanceID)
	sqlStr = strings.ReplaceAll(sqlStr, "{stay}", stayExpr(tx.Dialect.Name()))
	c.Logger().Debugf("Running query %s (scope instance=%q)", sqlQuery.Name, instanceID)
	store, err := queryStore(tx)
	if err != nil {
		return err
	}
	rows, err := store.Query(sqlStr, args...)
	if err != nil {
		c.Logger().Debugf("Error running query: %v", err)
		return fmt.Errorf("error running query: %s", err)
	}
	defer rows.Close()

	// save columns
	cols, err := rows.Columns()
	if err != nil {
		log.Printf("Error getting columns name: %v", err)
		return fmt.Errorf("error getting columns name: %v", err)
	}

	c.Response().Header().Add("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	filename := sqlQuery.Name
	if instanceID != "" {
		filename += "_" + instanceID
	}
	c.Response().Header().Add("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.xlsx"`, filename))

	lastRow, err := writeSheetRows(c, f, sqlQuery, rows, cols)
	if err != nil {
		return err
	}

	return writeExcelResponse(c, f, sqlQuery.Template, sqlQuery.Sheet, lastRow, len(cols))
}

// writeSheetRows writes the header and data rows of the query result into
// the template sheet and returns the last written row number (1 = header
// only).
func writeSheetRows(c buffalo.Context, f *excelize.File, sqlQuery *Queries, rows *sql.Rows, cols []string) (int, error) {
	line := 1
	for i, co := range cols {
		pos := sheetPosition(line, i+1)
		if err := f.SetCellValue(sqlQuery.Sheet, pos, co); err != nil {
			c.Logger().Debugf("error exporting to cell %s: %v", pos, err)
			return line, fmt.Errorf("error exporting to cell %s: %s", pos, err)
		}
	}

	values := make([]interface{}, len(cols))
	valuePtrs := make([]interface{}, len(cols))

	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		line = line + 1

		if err := rows.Scan(valuePtrs...); err != nil {
			log.Printf("Error fetching results: %v", err)
			return line, fmt.Errorf("error fetching columns: %v", err)
		}

		for i, co := range values {
			pos := sheetPosition(line, i+1)
			if err := f.SetCellValue(sqlQuery.Sheet, pos, co); err != nil {
				c.Logger().Debugf("error exporting to cell %s: %v", pos, err)
				return line, fmt.Errorf("error exporting to cell %s: %s", pos, err)
			}
		}
	}
	return line, nil
}

// writeExcelResponse applies the Bug 5 pivot-cache fixups to f, serializes
// the workbook, patches the final archive (template row truncation +
// _FilterDatabase defined name + Bug 7 template styles.xml restore) and
// writes it to the response.
func writeExcelResponse(c buffalo.Context, f *excelize.File, template, sheet string, lastRow, nCols int) error {
	// Update pivot caches to reference the data range actually written and
	// force Excel to refresh them on open (Bug 5). Without this, the cached
	// template range/records make Excel show a "repair/recover" prompt.
	lastCol := sheetPosition(1, nCols)
	lastCol = strings.TrimRight(lastCol, "0123456789")

	if err := updatePivotCaches(f, sheet, lastRow, lastCol); err != nil {
		c.Logger().Debugf("warning: failed to update pivot caches: %v", err)
	}

	// Serialize, then patch the final archive: drop leftover template rows
	// below the written range and update the _xlnm._FilterDatabase defined
	// name (both live in parts excelize re-serializes on WriteTo).
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		c.Logger().Debugf("Failed writing excel file: %v", err)
		return fmt.Errorf("failed writing excel file: %s", err)
	}

	out, err := truncateSheetRowsInZip(buf.Bytes(), sheet, lastRow, lastCol)
	if err != nil {
		c.Logger().Debugf("warning: failed to finalize export archive: %v", err)
		out = buf.Bytes()
	}

	// Bug 7: excelize's re-serialized styles.xml makes Excel flag the file
	// for repair. Replace it with the template's original part, keeping only
	// the cellXfs entries excelize legitimately appended while writing cells.
	if tplStyles, err := templateStylesXML(template); err != nil {
		c.Logger().Debugf("warning: failed to read template styles.xml: %v", err)
	} else if patched, err := restoreStylesInZip(out, tplStyles); err != nil {
		c.Logger().Debugf("warning: failed to restore template styles.xml: %v", err)
	} else {
		out = patched
	}

	if cnt, err := c.Response().Write(out); err != nil {
		c.Logger().Debugf("Failed writing excel file: %v", err)
		return fmt.Errorf("failed writing excel file: %s", err)
	} else {
		c.Logger().Debugf("wrote %d bytes to file", cnt)
	}

	return nil
}
