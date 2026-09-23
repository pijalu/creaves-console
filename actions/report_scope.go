package actions

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"creaves-console/models"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
)

type ReportScope struct{ InstanceID string }

func ResolveReportScope(instanceID string) ReportScope { return ReportScope{InstanceID: instanceID} }
func (s ReportScope) IsGlobal() bool                   { return s.InstanceID == "" }
func (s ReportScope) SQL() (string, []interface{}) {
	if s.IsGlobal() {
		return "", nil
	}
	return " instance_id = ? ", []interface{}{s.InstanceID}
}
func (s ReportScope) String() string {
	if s.IsGlobal() {
		return "global"
	}
	return fmt.Sprintf("instance:%s", s.InstanceID)
}

// ScopedWhere appends instance predicate to an existing WHERE fragment.
func ScopedWhere(scope ReportScope, base string) (string, []interface{}) {
	if scope.IsGlobal() {
		return base, nil
	}
	if base == "" {
		return "WHERE instance_id = ?", []interface{}{scope.InstanceID}
	}
	return base + " AND instance_id = ?", []interface{}{scope.InstanceID}
}

// ScopedWhereYear appends instance and optional year predicates to an
// existing WHERE fragment ("" or "WHERE ..."). year <= 0 adds no predicate.
// Used by the report pages so every report can run for a specific year
// (bugs.md bug 4).
func ScopedWhereYear(scope ReportScope, base string, year int) (string, []interface{}) {
	where, args := ScopedWhere(scope, base)
	if year > 0 {
		if where == "" {
			where = "WHERE year = ?"
		} else {
			where += " AND year = ?"
		}
		args = append(args, year)
	}
	return where, args
}

// parseReportYear reads the optional "year" request parameter shared by all
// report pages. Returns 0 (no filter) when absent/invalid.
func parseReportYear(c buffalo.Context) int {
	y, err := strconv.Atoi(strings.TrimSpace(c.Param("year")))
	if err != nil || y < 1900 || y > 2100 {
		return 0
	}
	return y
}

// reportYearOptions builds the year dropdown entries for a report page:
// every distinct year present in consolidated_animals for the scope.
func reportYearOptions(tx *pop.Connection, scope ReportScope, selected int) ([]yearOption, error) {
	var yearsRows []struct {
		Year int `db:"year"`
	}
	yearWhere, yearArgs := ScopedWhere(scope, "")
	if yearWhere == "" {
		yearWhere = "WHERE year > 0"
	} else {
		yearWhere += " AND year > 0"
	}
	if err := tx.RawQuery("SELECT DISTINCT year FROM consolidated_animals "+yearWhere+" ORDER BY year DESC", yearArgs...).All(&yearsRows); err != nil {
		return nil, err
	}
	years := make([]yearOption, 0, len(yearsRows))
	for _, y := range yearsRows {
		years = append(years, yearOption{Year: y.Year, Selected: y.Year == selected})
	}
	return years, nil
}

// yearOption is one entry of a year dropdown.
type yearOption struct {
	Year     int
	Selected bool
}

func reportScope(c buffalo.Context, tx *pop.Connection) (ReportScope, error) {
	scope := ResolveReportScope(c.Param("instance_id"))
	if scope.IsGlobal() {
		return scope, nil
	}
	if exists, err := tx.Where("instance_id = ?", scope.InstanceID).Exists(&models.CreavesInstance{}); err != nil {
		return scope, err
	} else if !exists {
		return scope, c.Error(http.StatusNotFound, fmt.Errorf("unknown instance: %s", scope.InstanceID))
	}
	return scope, nil
}
