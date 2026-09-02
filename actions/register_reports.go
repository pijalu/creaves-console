package actions

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"creaves-console/models"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
)

// Yearly register + register snapshot (Console side).
//
// Mirrors the Creaves /registertable and /registersnapshot pages, but over
// the denormalized consolidated_animals table and scoped via reportScope:
// all registered instances (default) or a single instance.
//
//   - Register (reports/register): all animals of one register year,
//     paginated, with a total count and CSV export. Year and center filters.
//   - Snapshot (reports/snapshot): animals present in care at a given date
//     (intake on or before the date, outtake on or after the date), capped
//     at registerSnapshotLimit rows like the Creaves original, with CSV
//     export. Date and center filters.
//
// Canonical (French) stored values are rendered through the localized
// helpers of the consolidated view, like the rest of the console.

// registerDateFmt is the display/CSV date format, matching the consolidated
// animals list.
const registerDateFmt = "02/01/2006"

// registerSnapshotLimit caps snapshot rows, mirroring the Creaves query.
const registerSnapshotLimit = 2000

// registerFmtDate renders a nullable time for tables and CSV.
func registerFmtDate(t nulls.Time) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(registerDateFmt)
}

// registerString renders a nullable string for CSV.
func registerString(s nulls.String) string {
	if !s.Valid {
		return ""
	}
	return s.String
}

// selectRegisterYear resolves the requested year (latest year present in
// consolidated_animals by default) and flags the selected dropdown entry.
// Reuses the annual report year logic.
func selectRegisterYear(c buffalo.Context, tx *pop.Connection, scope ReportScope) ([]annualYearOption, int, error) {
	return selectAnnualYear(c, tx, scope)
}

// registerReportData holds the shared handler logic of the register HTML
// page and its CSV export: resolves scope, year, dropdown options, total
// and the (unpaginated) animal rows for the CSV; the HTML handler applies
// pagination on top of the same query.
type registerReportData struct {
	Tx           *pop.Connection
	Scope        ReportScope
	Years        []annualYearOption
	Instances    []annualInstanceOption
	SelectedYear int
	Total        int
}

func loadRegisterReportData(c buffalo.Context) (registerReportData, error) {
	data := registerReportData{}
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return data, fmt.Errorf("no transaction found")
	}
	data.Tx = tx

	scope, err := reportScope(c, tx)
	if err != nil {
		return data, err
	}
	data.Scope = scope

	if data.Instances, err = listAnnualInstances(tx, scope); err != nil {
		return data, err
	}
	if data.Years, data.SelectedYear, err = selectRegisterYear(c, tx, scope); err != nil {
		return data, err
	}
	if data.SelectedYear == 0 {
		return data, nil
	}
	if data.Total, err = registerYearCount(tx, scope, data.SelectedYear); err != nil {
		return data, err
	}
	return data, nil
}

// registerBaseQuery returns the year+scope filtered query shared by the
// paginated page and the CSV export.
func (d registerReportData) registerBaseQuery() *pop.Query {
	q := d.Tx.Where("year = ?", d.SelectedYear)
	if !d.Scope.IsGlobal() {
		q = q.Where("instance_id = ?", d.Scope.InstanceID)
	}
	return q
}

// registerYearCount counts the animals of one year for the current scope.
func registerYearCount(tx *pop.Connection, scope ReportScope, year int) (int, error) {
	where, args := ScopedWhere(scope, "WHERE year = ?")
	args = append([]interface{}{year}, args...)
	var total int
	if err := tx.RawQuery("SELECT COUNT(DISTINCT id) FROM consolidated_animals "+where, args...).First(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// RegisterIndex handles GET /reports/register?year=YYYY&instance_id=….
func RegisterIndex(c buffalo.Context) error {
	data, err := loadRegisterReportData(c)
	if err != nil {
		return err
	}
	animals := &models.ConsolidatedAnimals{}
	if data.SelectedYear != 0 {
		q := data.registerBaseQuery().PaginateFromParams(c.Params())
		if err := q.Order("year_number desc").All(animals); err != nil {
			return err
		}
		c.Set("pagination", q.Paginator)
	}
	c.Set("years", data.Years)
	c.Set("instances", data.Instances)
	c.Set("selectedYear", data.SelectedYear)
	c.Set("instanceID", data.Scope.InstanceID)
	c.Set("total", data.Total)
	c.Set("animals", animals)
	return c.Render(http.StatusOK, r.HTML("reports/register.plush.html"))
}

// RegisterExportCSV handles GET /reports/register/export.csv?year=YYYY&instance_id=….
// One row per animal: instance, number, type, species, identification,
// entry date, discovery location, age, reason, outtake date, outtake
// reason, outtake location.
func RegisterExportCSV(c buffalo.Context) error {
	data, err := loadRegisterReportData(c)
	if err != nil {
		return err
	}
	if data.SelectedYear == 0 {
		return fmt.Errorf("year not provided")
	}

	animals := &models.ConsolidatedAnimals{}
	if err := data.registerBaseQuery().Order("year_number desc").All(animals); err != nil {
		return err
	}

	header := append([]string{"Instance"}, registerCSVHeader(requestUILang(c))...)
	rows := make([][]string, 0, len(*animals))
	for _, a := range *animals {
		rows = append(rows, []string{
			a.InstanceID,
			strconv.Itoa(a.YearNumber),
			registerString(a.AnimalType),
			registerString(a.Species),
			registerString(a.Ring),
			registerFmtDate(a.IntakeDate),
			registerString(a.DiscoveryLocation),
			registerString(a.AnimalAge),
			registerString(a.EntryCause),
			registerFmtDate(a.OuttakeDate),
			registerString(a.OuttakeType),
			registerString(a.OuttakeLocation),
		})
	}

	filename := fmt.Sprintf("registertable-%d.csv", data.SelectedYear)
	if !data.Scope.IsGlobal() {
		filename = fmt.Sprintf("registertable-%d-%s.csv", data.SelectedYear, data.Scope.InstanceID)
	}
	return writeCSV(c, filename, header, rows)
}

// registerCSVHeader returns the localized CSV header row (after the
// leading Instance column).
func registerCSVHeader(lang string) []string {
	switch lang {
	case "fr":
		return []string{"Numéro", "Type", "Espèce", "Identification", "Date d'entrée", "Lieu de découverte", "Âge", "Motif", "Date de sortie", "Motif de sortie", "Lieu"}
	case "de":
		return []string{"Nummer", "Typ", "Art", "Kennung", "Eintrittsdatum", "Fundort", "Alter", "Grund", "Abgangsdatum", "Abgangsgrund", "Ort"}
	case "nl":
		return []string{"Nummer", "Type", "Soort", "Identificatie", "Binnenkomstdatum", "Vindplaats", "Leeftijd", "Reden", "Uitgangsdatum", "Uitgangsreden", "Plaats"}
	default:
		return []string{"Number", "Type", "Species", "Identification", "Entry Date", "Discovery Location", "Age", "Reason", "Outtake date", "Outtake reason", "Location"}
	}
}

// parseSnapshotDate accepts the native date-input format (2006-01-02) and
// the Creaves-style format (2006/01/02).
func parseSnapshotDate(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", "2006/01/02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid snapshot date %q (expected YYYY-MM-DD)", s)
}

// snapshotReportData holds the shared handler logic of the snapshot HTML
// page and its CSV export.
type snapshotReportData struct {
	Tx           *pop.Connection
	Scope        ReportScope
	Instances    []annualInstanceOption
	SnapshotDate string // ISO YYYY-MM-DD as shown in the date input
	Total        int
	animals      func() (*models.ConsolidatedAnimals, error)
}

// loadSnapshotAnimals runs the snapshot query (capped at
// registerSnapshotLimit rows).
func (d snapshotReportData) loadSnapshotAnimals() (*models.ConsolidatedAnimals, error) {
	return d.animals()
}

func loadSnapshotReportData(c buffalo.Context) (snapshotReportData, error) {
	data := snapshotReportData{}
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return data, fmt.Errorf("no transaction found")
	}
	data.Tx = tx

	scope, err := reportScope(c, tx)
	if err != nil {
		return data, err
	}
	data.Scope = scope

	if data.Instances, err = listAnnualInstances(tx, scope); err != nil {
		return data, err
	}

	snapshotDate := time.Now().Format("2006-01-02")
	if p := c.Param("snapshotDate"); p != "" {
		snapshotDate = p
	}
	day, err := parseSnapshotDate(snapshotDate)
	if err != nil {
		return data, err
	}
	data.SnapshotDate = day.Format("2006-01-02")
	c.Set("snapshotDate", data.SnapshotDate)

	dayEnd := day.AddDate(0, 0, 1)
	where, args := ScopedWhere(scope,
		"WHERE intake_date IS NOT NULL AND intake_date < ? AND (outtake_date IS NULL OR outtake_date >= ?)")
	args = append([]interface{}{dayEnd, day}, args...)
	if err := tx.RawQuery("SELECT COUNT(*) FROM consolidated_animals "+where, args...).First(&data.Total); err != nil {
		return data, err
	}
	data.animals = func() (*models.ConsolidatedAnimals, error) {
		animals := &models.ConsolidatedAnimals{}
		q := "SELECT * FROM consolidated_animals " + where + " ORDER BY year DESC, year_number DESC LIMIT ?"
		qargs := append(args, registerSnapshotLimit)
		if err := tx.RawQuery(q, qargs...).All(animals); err != nil {
			return nil, err
		}
		return animals, nil
	}
	return data, nil
}

// SnapshotIndex handles GET /reports/snapshot?snapshotDate=YYYY-MM-DD&instance_id=….
func SnapshotIndex(c buffalo.Context) error {
	data, err := loadSnapshotReportData(c)
	if err != nil {
		return err
	}
	animals, err := data.loadSnapshotAnimals()
	if err != nil {
		return err
	}
	c.Set("instances", data.Instances)
	c.Set("snapshotDate", data.SnapshotDate)
	c.Set("instanceID", data.Scope.InstanceID)
	c.Set("total", data.Total)
	c.Set("limit", registerSnapshotLimit)
	c.Set("animals", animals)
	return c.Render(http.StatusOK, r.HTML("reports/snapshot.plush.html"))
}

// SnapshotExportCSV handles GET
// /reports/snapshot/export.csv?snapshotDate=YYYY-MM-DD&instance_id=….
// One row per animal present in care: instance, number, type, species,
// identification, zone, cage, entry date, discovery location, age, reason.
func SnapshotExportCSV(c buffalo.Context) error {
	data, err := loadSnapshotReportData(c)
	if err != nil {
		return err
	}
	animals, err := data.loadSnapshotAnimals()
	if err != nil {
		return err
	}

	header := append([]string{"Instance"}, snapshotCSVHeader(requestUILang(c))...)
	rows := make([][]string, 0, len(*animals))
	for _, a := range *animals {
		rows = append(rows, []string{
			a.InstanceID,
			strconv.Itoa(a.YearNumber),
			registerString(a.AnimalType),
			registerString(a.Species),
			registerString(a.Ring),
			registerString(a.Zone),
			registerString(a.Cage),
			registerFmtDate(a.IntakeDate),
			registerString(a.DiscoveryLocation),
			registerString(a.AnimalAge),
			registerString(a.EntryCause),
		})
	}

	filename := fmt.Sprintf("registersnapshot-%s.csv", data.SnapshotDate)
	if !data.Scope.IsGlobal() {
		filename = fmt.Sprintf("registersnapshot-%s-%s.csv", data.SnapshotDate, data.Scope.InstanceID)
	}
	return writeCSV(c, filename, header, rows)
}

// snapshotCSVHeader returns the localized CSV header row (after the
// leading Instance column).
func snapshotCSVHeader(lang string) []string {
	switch lang {
	case "fr":
		return []string{"Numéro", "Type", "Espèce", "Identification", "Zone", "Cage", "Date d'entrée", "Lieu de découverte", "Âge", "Motif"}
	case "de":
		return []string{"Nummer", "Typ", "Art", "Kennung", "Zone", "Käfig", "Eintrittsdatum", "Fundort", "Alter", "Grund"}
	case "nl":
		return []string{"Nummer", "Type", "Soort", "Identificatie", "Zone", "Kooi", "Binnenkomstdatum", "Vindplaats", "Leeftijd", "Reden"}
	default:
		return []string{"Number", "Type", "Species", "Identification", "Zone", "Cage", "Entry Date", "Discovery Location", "Age", "Reason"}
	}
}
