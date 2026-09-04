package actions

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/plush/v4"
	"github.com/gobuffalo/pop/v6"

	"creaves-console/models"
)

// consolidatedSortColumns maps the public `sort` parameter to a fixed ORDER BY
// SQL fragment for the consolidated animal register. Only keys present in this
// whitelist ever reach the database — user input is used as a map lookup,
// never inside SQL.
var consolidatedSortColumns = map[string]string{
	"instance":           "instance_id",
	"year":               "year",
	"number":             "year_number",
	"species":            "species",
	"identification":     "ring",
	"intake_date":        "intake_date",
	"discovery_date":     "discovery_date",
	"discovery_location": "discovery_location",
	"postal_code":        "discovery_postal_code",
	"city":               "discovery_city",
	"entry_cause":        "entry_cause",
	"outcome":            "current_status",
	"outcome_type":       "outtake_type",
	"outcome_date":       "outtake_date",
	"outcome_location":   "outtake_location",
}

// applyConsolidatedSort applies the `sort`/`dir` request parameters to q,
// falling back to the default register order when the parameters are absent
// or invalid.
func applyConsolidatedSort(q *pop.Query, c buffalo.Context) *pop.Query {
	for _, clause := range consolidatedSortClauses(c.Param("sort"), c.Param("dir")) {
		q = q.Order(clause)
	}
	return q
}

// consolidatedSortClauses is the pure core mapping a sort key and direction to
// ORDER BY SQL fragments.
func consolidatedSortClauses(sortKey, dir string) []string {
	col, ok := consolidatedSortColumns[sortKey]
	if !ok {
		return []string{"year desc", "year_number asc"}
	}
	dir = strings.ToLower(dir)
	if dir != "asc" && dir != "desc" {
		dir = "asc"
	}
	return []string{col + " " + dir, "year desc", "year_number asc"}
}

// sortParams extracts the current `sort`/`dir` pair from the request inside a
// plush helper. Missing request (tests, minimal renders) yields defaults.
func sortParams(help plush.HelperContext) (sortKey, dir string) {
	req, _ := help.Value("request").(*http.Request)
	if req == nil {
		return "", ""
	}
	sortKey = req.URL.Query().Get("sort")
	dir = strings.ToLower(req.URL.Query().Get("dir"))
	if dir != "asc" && dir != "desc" {
		dir = ""
	}
	return sortKey, dir
}

// sortLink is a template helper: href for a sortable column header link.
// Clicking the active column toggles the direction; any other column sorts
// ascending. Existing query parameters (filters, page) are preserved, but the
// page resets to 1 because the row order changes.
func sortLink(field string, help plush.HelperContext) (template.HTML, error) {
	req, _ := help.Value("request").(*http.Request)
	if req == nil {
		return template.HTML("#"), nil
	}
	vals := req.URL.Query()
	cur, dir := sortParams(help)
	next := "asc"
	if cur == field && dir == "asc" {
		next = "desc"
	}
	vals.Set("sort", field)
	vals.Set("dir", next)
	vals.Del("page")
	return template.HTML(req.URL.Path + "?" + vals.Encode()), nil
}

// sortIcon is a template helper: ▲/▼ for the active sort column, empty for
// inactive ones.
func sortIcon(field string, help plush.HelperContext) (template.HTML, error) {
	cur, dir := sortParams(help)
	if cur != field {
		return template.HTML(""), nil
	}
	if dir == "desc" {
		return template.HTML("▼"), nil
	}
	return template.HTML("▲"), nil
}

// outcomeClassLabels maps outcome status codes to Bootstrap badge classes.
var outcomeClassLabels = map[string]string{
	"positive": "success",
	"neutral":  "secondary",
	"negative": "danger",
}

// outcomeTextLabels maps outcome status codes to localized UI labels.
var outcomeTextLabels = map[string]map[string]string{
	"positive": {
		"en-US": "Positive", "fr": "Positive", "de": "Positiv", "nl": "Positief",
	},
	"neutral": {
		"en-US": "Neutral", "fr": "Neutre", "de": "Neutral", "nl": "Neutraal",
	},
	"negative": {
		"en-US": "Negative", "fr": "Négative", "de": "Negativ", "nl": "Negatief",
	},
}

// asConsolidatedAnimal normalizes a template argument to a
// ConsolidatedAnimal value. Plush passes the index loop variable by value but
// the show handler passes a *models.ConsolidatedAnimal, so both shapes must
// be accepted.
func asConsolidatedAnimal(v interface{}) (models.ConsolidatedAnimal, bool) {
	switch a := v.(type) {
	case models.ConsolidatedAnimal:
		return a, true
	case *models.ConsolidatedAnimal:
		if a == nil {
			return models.ConsolidatedAnimal{}, false
		}
		return *a, true
	}
	return models.ConsolidatedAnimal{}, false
}

// outcomeClass is a template helper: Bootstrap badge class for the stored
// outcome of a consolidated animal ("secondary" when unknown). Accepts both
// the value (index loop variable) and the pointer (show handler) form.
func outcomeClass(v interface{}, help plush.HelperContext) (template.HTML, error) {
	a, ok := asConsolidatedAnimal(v)
	if !ok {
		return template.HTML(""), fmt.Errorf("outcomeClass: expected models.ConsolidatedAnimal, got %T", v)
	}
	if class, ok := outcomeClassLabels[a.OutcomeStatus()]; ok {
		return template.HTML(class), nil
	}
	return template.HTML("secondary"), nil
}

// outcomeLabel is a template helper: localized Positive/Neutral/Negative label
// for the stored outcome (empty string when unknown). Accepts both the value
// and pointer form of the model.
func outcomeLabel(v interface{}, help plush.HelperContext) (string, error) {
	a, ok := asConsolidatedAnimal(v)
	if !ok {
		return "", fmt.Errorf("outcomeLabel: expected models.ConsolidatedAnimal, got %T", v)
	}
	status := a.OutcomeStatus()
	if labels, ok := outcomeTextLabels[status]; ok {
		if label, ok := labels[currentUILang(help)]; ok {
			return label, nil
		}
	}
	return status, nil
}
