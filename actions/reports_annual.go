package actions

import (
	"fmt"
	"net/http"
	"strconv"

	"creaves-console/models"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
)

// Annual statistics report (plan §5, Console side).
//
// Same twelve tables as the Creaves annual report, but computed over the
// denormalized consolidated_animals table (one row per animal per instance)
// and scoped via reportScope: all registered instances (default) or a single
// instance. Every table counts DISTINCT consolidated rows (COUNT(DISTINCT
// id)) for the selected year, groups NULL/empty categories into an "Unknown"
// bucket so columns sum to T, and shows the percentage of the table total T
// with 1 decimal.
//
// §5.2 mapping (console source columns): species, species_class,
// species_agw_group, species_subside_group, species_native_status,
// animal_age, outtake_type, outtake_rating, outtake_dead, entry_cause,
// entry_cause_detail, entry_cause_nature. The outtake tables (type, rating,
// dead vs released) are restricted to animals with an outtake
// (outtake_type IS NOT NULL), mirroring the INNER JOIN on the Creaves side.

// annualStatRow is one category line of a statistics table.
type annualStatRow struct {
	Category string `db:"category"`
	Count    int    `db:"n"`
	Percent  string
}

// annualStatSection is one of the 12 statistics tables.
type annualStatSection struct {
	ID    string // localization key suffix + anchor
	Title string // localized section title (computed per request language)
	Rows  []annualStatRow
	Total int
}

// annualStatQuery couples a grouped query (category, n) with the matching
// total query (T) for the same FROM/WHERE. Both must contain a %s placeholder
// for the WHERE clause (scope + year predicates appended by the runner).
type annualStatQuery struct {
	id      string
	grouped string
	total   string
}

// annualUnknown groups NULL and empty-string categories into one bucket.
func annualUnknown(col string) string {
	return `COALESCE(NULLIF(` + col + `, ''), 'Unknown')`
}

// annualHasOuttake restricts a table to animals with an outtake.
const annualHasOuttake = `outtake_type IS NOT NULL`

func annualStatQueries() []annualStatQuery {
	base := `FROM consolidated_animals`
	count := `COUNT(DISTINCT id)`
	return []annualStatQuery{
		{
			id: "species",
			grouped: `SELECT ` + annualUnknown("species") + ` AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY n DESC, category ASC LIMIT 20`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
		{
			id: "class",
			grouped: `SELECT ` + annualUnknown("species_class") + ` AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY n DESC, category ASC`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
		{
			id: "agw_group",
			grouped: `SELECT ` + annualUnknown("species_agw_group") + ` AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY n DESC, category ASC`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
		{
			id: "subsidies_group",
			grouped: `SELECT ` + annualUnknown("species_subside_group") + ` AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY n DESC, category ASC`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
		{
			id: "native_status",
			grouped: `SELECT ` + annualUnknown("species_native_status") + ` AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY n DESC, category ASC`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
		{
			id: "entry_age",
			grouped: `SELECT ` + annualUnknown("animal_age") + ` AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY n DESC, category ASC`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
		{
			id: "outtake_type",
			grouped: `SELECT ` + annualUnknown("outtake_type") + ` AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY n DESC, category ASC`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
		{
			id: "outtake_rating",
			grouped: `SELECT CASE outtake_rating
				WHEN -1 THEN 'Dead'
				WHEN 0 THEN 'Neutral'
				WHEN 1 THEN 'Alive'
				ELSE 'Unknown' END AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY MIN(outtake_rating) ASC`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
		{
			id: "outtake_dead_released",
			grouped: `SELECT CASE WHEN outtake_dead = 1 THEN 'Dead' ELSE 'Released' END AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY MIN(outtake_dead) DESC`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
		{
			id: "entry_cause",
			grouped: `SELECT ` + annualUnknown("entry_cause") + ` AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY n DESC, category ASC`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
		{
			id: "entry_cause_detail",
			grouped: `SELECT ` + annualUnknown("entry_cause_detail") + ` AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY n DESC, category ASC`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
		{
			id: "entry_cause_nature",
			grouped: `SELECT ` + annualUnknown("entry_cause_nature") + ` AS category, ` + count + ` AS n ` + base + `
				%s GROUP BY 1 ORDER BY n DESC, category ASC`,
			total: `SELECT ` + count + ` ` + base + ` %s`,
		},
	}
}

// annualStatPercent formats count as a percentage of total with 1 decimal.
func annualStatPercent(count, total int) string {
	if total == 0 {
		return "0.0"
	}
	return fmt.Sprintf("%.1f", float64(count)*100.0/float64(total))
}

// annualSectionTitles maps section IDs to localized titles per UI language.
func annualSectionTitles(lang string) map[string]string {
	switch lang {
	case "fr":
		return map[string]string{
			"species": "Top 20 espèces", "class": "Classe", "agw_group": "Groupe AGW",
			"subsidies_group": "Groupe subsides", "native_status": "Indigénat",
			"entry_age": "Âge à l'entrée", "outtake_type": "Sorties par type",
			"outtake_rating": "Sorties par évaluation", "outtake_dead_released": "Sorties : morts vs relâchés",
			"entry_cause": "Cause d'entrée", "entry_cause_detail": "Cause d'entrée (détail)",
			"entry_cause_nature": "Nature des causes d'entrée",
		}
	case "de":
		return map[string]string{
			"species": "Top 20 Arten", "class": "Klasse", "agw_group": "AGW-Gruppe",
			"subsidies_group": "Subventionsgruppe", "native_status": "Heimischer Status",
			"entry_age": "Alter bei Eintritt", "outtake_type": "Abgänge nach Typ",
			"outtake_rating": "Abgänge nach Bewertung", "outtake_dead_released": "Abgänge: tot vs freigelassen",
			"entry_cause": "Eintrittsursache", "entry_cause_detail": "Eintrittsursache (Detail)",
			"entry_cause_nature": "Art der Eintrittsursachen",
		}
	case "nl":
		return map[string]string{
			"species": "Top 20 soorten", "class": "Klasse", "agw_group": "AGW-groep",
			"subsidies_group": "Subsidiegroep", "native_status": "Inheemse status",
			"entry_age": "Leeftijd bij binnenkomst", "outtake_type": "Uitgangen per type",
			"outtake_rating": "Uitgangen per beoordeling", "outtake_dead_released": "Uitgangen: dood vs vrijgelaten",
			"entry_cause": "Reden van binnenkomst", "entry_cause_detail": "Reden van binnenkomst (detail)",
			"entry_cause_nature": "Aard van de redenen",
		}
	default:
		return map[string]string{
			"species": "Top 20 species", "class": "Class", "agw_group": "AGW group",
			"subsidies_group": "Subsidies group", "native_status": "Native status",
			"entry_age": "Age at entry", "outtake_type": "Outtake by type",
			"outtake_rating": "Outtake by rating", "outtake_dead_released": "Outtake: dead vs released",
			"entry_cause": "Entry cause", "entry_cause_detail": "Entry cause (detail)",
			"entry_cause_nature": "Entry cause nature",
		}
	}
}

// annualCSVHeader returns the localized CSV header row.
func annualCSVHeader(lang string) []string {
	switch lang {
	case "fr":
		return []string{"Année", "Instance", "Section", "Catégorie", "Nombre", "%"}
	case "de":
		return []string{"Jahr", "Instanz", "Abschnitt", "Kategorie", "Anzahl", "%"}
	case "nl":
		return []string{"Jaar", "Instantie", "Sectie", "Categorie", "Aantal", "%"}
	default:
		return []string{"Year", "Instance", "Section", "Category", "Count", "%"}
	}
}

// annualScopeLabel is the CSV/instance label for the current scope.
func annualScopeLabel(scope ReportScope, lang string) string {
	if !scope.IsGlobal() {
		return scope.InstanceID
	}
	switch lang {
	case "fr":
		return "Tous les centres"
	case "de":
		return "Alle Zentren"
	case "nl":
		return "Alle centra"
	default:
		return "All centers"
	}
}

// runAnnualStatQuery executes one grouped query + its total query for the
// given year and scope. extra is an optional additional predicate (e.g. the
// outtake restriction).
func runAnnualStatQuery(tx *pop.Connection, q annualStatQuery, year int, scope ReportScope, extra string) (annualStatSection, error) {
	sec := annualStatSection{ID: q.id, Rows: []annualStatRow{}}

	base := "WHERE year = ?"
	args := []interface{}{year}
	if extra != "" {
		base += " AND " + extra
	}
	where, scopeArgs := ScopedWhere(scope, base)
	args = append(args, scopeArgs...)

	if err := tx.RawQuery(fmt.Sprintf(q.grouped, where), args...).All(&sec.Rows); err != nil {
		return sec, fmt.Errorf("annual stat %s: %w", q.id, err)
	}
	var total int
	if err := tx.RawQuery(fmt.Sprintf(q.total, where), args...).First(&total); err != nil {
		return sec, fmt.Errorf("annual stat %s total: %w", q.id, err)
	}
	sec.Total = total
	for i := range sec.Rows {
		sec.Rows[i].Percent = annualStatPercent(sec.Rows[i].Count, total)
	}
	return sec, nil
}

// runAnnualStats computes all 12 statistics tables for year and scope.
func runAnnualStats(tx *pop.Connection, year int, scope ReportScope, lang string) ([]annualStatSection, error) {
	queries := annualStatQueries()
	titles := annualSectionTitles(lang)
	sections := make([]annualStatSection, 0, len(queries))
	for _, q := range queries {
		extra := ""
		switch q.id {
		case "outtake_type", "outtake_rating", "outtake_dead_released":
			extra = annualHasOuttake
		}
		sec, err := runAnnualStatQuery(tx, q, year, scope, extra)
		if err != nil {
			return nil, err
		}
		sec.Title = titles[q.id]
		sections = append(sections, sec)
	}
	return sections, nil
}

// annualYearOption is one entry of the year dropdown.
type annualYearOption struct {
	Year     int
	Selected bool
}

// annualInstanceOption is one entry of the instance scope dropdown.
type annualInstanceOption struct {
	InstanceID string // "" = all centers
	Name       string
	Selected   bool
}

// selectAnnualYear resolves the requested year (or the latest year present in
// consolidated_animals by default) and flags the selected dropdown entry.
func selectAnnualYear(c buffalo.Context, tx *pop.Connection, scope ReportScope) ([]annualYearOption, int, error) {
	var years []struct {
		Year int `db:"year"`
	}
	yearWhere, yearArgs := ScopedWhere(scope, "")
	if err := tx.RawQuery("SELECT DISTINCT year FROM consolidated_animals "+yearWhere+" ORDER BY year DESC", yearArgs...).All(&years); err != nil {
		return nil, 0, err
	}
	options := make([]annualYearOption, 0, len(years))
	for _, y := range years {
		options = append(options, annualYearOption{Year: y.Year})
	}
	if len(options) == 0 {
		return options, 0, nil
	}
	requested, parseErr := strconv.Atoi(c.Param("year"))
	selected := 0
	for i := range options {
		if parseErr == nil && options[i].Year == requested {
			options[i].Selected = true
			selected = options[i].Year
		}
	}
	if selected == 0 {
		options[0].Selected = true // years are DESC: latest first
		selected = options[0].Year
	}
	return options, selected, nil
}

// listAnnualInstances builds the instance scope dropdown entries: "All
// centers" (empty value, default) plus one entry per registered instance.
func listAnnualInstances(tx *pop.Connection, scope ReportScope) ([]annualInstanceOption, error) {
	instances := &models.CreavesInstances{}
	if err := tx.Order("instance_id asc").All(instances); err != nil {
		return nil, err
	}
	options := []annualInstanceOption{{InstanceID: "", Name: "", Selected: scope.IsGlobal()}}
	for _, inst := range *instances {
		options = append(options, annualInstanceOption{
			InstanceID: inst.InstanceID,
			Name:       inst.Name,
			Selected:   scope.InstanceID == inst.InstanceID,
		})
	}
	return options, nil
}

// annualReportData holds the shared handler logic of the HTML page and the
// CSV export: resolves scope, year, dropdown options and runs the stats.
func annualReportData(c buffalo.Context, lang string) (tx *pop.Connection, scope ReportScope, years []annualYearOption, instances []annualInstanceOption, selectedYear int, sections []annualStatSection, err error) {
	var ok bool
	tx, ok = c.Value("tx").(*pop.Connection)
	if !ok {
		return nil, scope, nil, nil, 0, nil, fmt.Errorf("no transaction found")
	}
	if scope, err = reportScope(c, tx); err != nil {
		return nil, scope, nil, nil, 0, nil, err
	}
	if instances, err = listAnnualInstances(tx, scope); err != nil {
		return nil, scope, nil, nil, 0, nil, err
	}
	if years, selectedYear, err = selectAnnualYear(c, tx, scope); err != nil {
		return nil, scope, nil, nil, 0, nil, err
	}
	sections = []annualStatSection{}
	if selectedYear != 0 {
		if sections, err = runAnnualStats(tx, selectedYear, scope, lang); err != nil {
			return nil, scope, nil, nil, 0, nil, err
		}
	}
	return tx, scope, years, instances, selectedYear, sections, nil
}

// ReportsAnnualIndex handles GET /reports/annual?year=YYYY&instance_id=….
func ReportsAnnualIndex(c buffalo.Context) error {
	_, scope, years, instances, selectedYear, sections, err := annualReportData(c, requestUILang(c))
	if err != nil {
		return err
	}
	c.Set("years", years)
	c.Set("instances", instances)
	c.Set("selectedYear", selectedYear)
	c.Set("instanceID", scope.InstanceID)
	c.Set("sections", sections)
	return c.Render(http.StatusOK, r.HTML("reports/annual.plush.html"))
}

// ReportsAnnualExportCSV handles GET /reports/annual/export.csv?year=YYYY&instance_id=….
// Single multi-section CSV: Year, Instance, Section, Category, Count, Percent.
func ReportsAnnualExportCSV(c buffalo.Context) error {
	lang := requestUILang(c)
	_, scope, _, _, selectedYear, sections, err := annualReportData(c, lang)
	if err != nil {
		return err
	}
	if selectedYear == 0 {
		return fmt.Errorf("year not provided")
	}

	yearStr := strconv.Itoa(selectedYear)
	scopeLabel := annualScopeLabel(scope, lang)
	header := annualCSVHeader(lang)
	rows := [][]string{}
	for _, sec := range sections {
		for _, row := range sec.Rows {
			rows = append(rows, []string{
				yearStr,
				scopeLabel,
				sec.Title,
				row.Category,
				strconv.Itoa(row.Count),
				row.Percent,
			})
		}
		rows = append(rows, []string{
			yearStr,
			scopeLabel,
			sec.Title,
			annualTotalLabel(lang),
			strconv.Itoa(sec.Total),
			"100.0",
		})
	}

	filename := fmt.Sprintf("reports-annual-%d.csv", selectedYear)
	if !scope.IsGlobal() {
		filename = fmt.Sprintf("reports-annual-%d-%s.csv", selectedYear, scope.InstanceID)
	}
	return writeCSV(c, filename, header, rows)
}

// annualTotalLabel returns the localized "Total" row label.
func annualTotalLabel(lang string) string {
	switch lang {
	case "fr":
		return "Total"
	case "de":
		return "Gesamt"
	case "nl":
		return "Totaal"
	default:
		return "Total"
	}
}
