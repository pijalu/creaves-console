package actions

import (
	"creaves-console/models"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
	"github.com/gobuffalo/x/responder"
)

// DashboardIndex displays the main dashboard for the consolidation app
func DashboardIndex(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}
	scope, err := reportScope(c, tx)
	if err != nil {
		return err
	}
	stats := make(map[string]interface{})
	var animalCount int
	if scope.IsGlobal() {
		animalCount, err = tx.Count(&models.ConsolidatedAnimal{})
	} else {
		animalCount, err = tx.Where("instance_id = ?", scope.InstanceID).Count(&models.ConsolidatedAnimal{})
	}
	stats["total_animals"] = animalCount
	if err != nil {
		return err
	}
	statusCounts := []struct {
		Status string `db:"current_status"`
		Count  int    `db:"count"`
	}{}
	where, args := ScopedWhere(scope, "")
	if err = tx.RawQuery("SELECT current_status, COUNT(*) as count FROM consolidated_animals "+where+" GROUP BY current_status", args...).All(&statusCounts); err != nil {
		return err
	}
	statusMap := make(map[string]int)
	for _, x := range statusCounts {
		statusMap[x.Status] = x.Count
	}
	stats["by_status"] = statusMap
	instanceCounts := []struct {
		InstanceID string `db:"instance_id"`
		Count      int    `db:"count"`
	}{}
	if err = tx.RawQuery("SELECT instance_id, COUNT(*) as count FROM consolidated_animals "+where+" GROUP BY instance_id", args...).All(&instanceCounts); err != nil {
		return err
	}
	instanceMap := make(map[string]int)
	for _, x := range instanceCounts {
		instanceMap[x.InstanceID] = x.Count
	}
	stats["by_instance"] = instanceMap
	var eventCount int
	if scope.IsGlobal() {
		eventCount, err = tx.Count(&models.EventStream{})
	} else {
		eventCount, err = tx.Where("instance_id = ?", scope.InstanceID).Count(&models.EventStream{})
	}
	stats["total_events"] = eventCount
	if err != nil {
		return err
	}
	unprocessedQuery := tx.Where("processed_at IS NULL")
	if !scope.IsGlobal() {
		unprocessedQuery = unprocessedQuery.Where("instance_id = ?", scope.InstanceID)
	}
	if stats["unprocessed_events"], err = unprocessedQuery.Count(&models.EventStream{}); err != nil {
		return err
	}
	uniqueQuery := "SELECT COUNT(DISTINCT instance_id) FROM event_streams"
	var uniqueArgs []interface{}
	if !scope.IsGlobal() {
		uniqueQuery += " WHERE instance_id = ?"
		uniqueArgs = append(uniqueArgs, scope.InstanceID)
	}
	var uniqueInstances int
	if err = tx.RawQuery(uniqueQuery, uniqueArgs...).First(&uniqueInstances); err != nil {
		return err
	}
	stats["unique_instances"] = uniqueInstances
	var keyCount int
	if scope.IsGlobal() {
		keyCount, err = tx.Count(&models.WebhookAPIKey{})
	} else {
		keyCount, err = tx.Where("instance_id = ?", scope.InstanceID).Count(&models.WebhookAPIKey{})
	}
	stats["total_webhook_keys"] = keyCount
	if err != nil {
		return err
	}
	activeKeyQuery := tx.Where("active = ?", true)
	if !scope.IsGlobal() {
		activeKeyQuery = activeKeyQuery.Where("instance_id = ?", scope.InstanceID)
	}
	if stats["active_webhook_keys"], err = activeKeyQuery.Count(&models.WebhookAPIKey{}); err != nil {
		return err
	}
	c.Set("stats", stats)
	c.Set("instanceID", scope.InstanceID)
	return c.Render(http.StatusOK, r.HTML("dashboard/index.plush.html"))
}

// ConsolidatedAnimalsIndex displays the consolidated animal list (Register view)
func ConsolidatedAnimalsIndex(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	animals := &models.ConsolidatedAnimals{}
	q := tx.PaginateFromParams(c.Params())
	scope, err := reportScope(c, tx)
	if err != nil {
		return err
	}
	if !scope.IsGlobal() {
		q = q.Where("instance_id = ?", scope.InstanceID)
	}

	// Apply filters
	viewMode := animalsViewMode(c, scope)
	q = applyConsolidatedAnimalFilters(q, c, viewMode, scope)

	if err := applyConsolidatedSort(q, c).All(animals); err != nil {
		return err
	}

	// Get filter options
	var instances []struct {
		InstanceID string `db:"instance_id"`
	}
	tx.RawQuery("SELECT DISTINCT instance_id FROM consolidated_animals ORDER BY instance_id").All(&instances)

	var speciesList []struct {
		Species string `db:"species"`
	}
	tx.RawQuery("SELECT DISTINCT species FROM consolidated_animals WHERE species IS NOT NULL ORDER BY species").All(&speciesList)

	var typesList []struct {
		AnimalType string `db:"animal_type"`
	}
	tx.RawQuery("SELECT DISTINCT animal_type FROM consolidated_animals WHERE animal_type IS NOT NULL ORDER BY animal_type").All(&typesList)

	var citiesList []struct {
		City string `db:"discovery_city"`
	}
	tx.RawQuery("SELECT DISTINCT discovery_city FROM consolidated_animals WHERE discovery_city IS NOT NULL ORDER BY discovery_city").All(&citiesList)

	// Years are rendered as strings so the template can compare them directly
	// against params["year"] (Plush `string(int)` yields "" — bugs.md #5).
	var yearRows []struct {
		Year int `db:"year"`
	}
	tx.RawQuery("SELECT DISTINCT year FROM consolidated_animals ORDER BY year DESC").All(&yearRows)
	yearsList := make([]string, 0, len(yearRows))
	for _, yr := range yearRows {
		yearsList = append(yearsList, strconv.Itoa(yr.Year))
	}

	var entryCausesList []struct {
		EntryCause string `db:"entry_cause"`
	}
	tx.RawQuery("SELECT DISTINCT entry_cause FROM consolidated_animals WHERE entry_cause IS NOT NULL ORDER BY entry_cause").All(&entryCausesList)

	var agesList []struct {
		AnimalAge string `db:"animal_age"`
	}
	tx.RawQuery("SELECT DISTINCT animal_age FROM consolidated_animals WHERE animal_age IS NOT NULL ORDER BY animal_age").All(&agesList)

	var outtakeTypesList []struct {
		OuttakeType string `db:"outtake_type"`
	}
	tx.RawQuery("SELECT DISTINCT outtake_type FROM consolidated_animals WHERE outtake_type IS NOT NULL ORDER BY outtake_type").All(&outtakeTypesList)

	// Localized dropdown labels for translatable fields
	uiLang := requestUILang(c)
	entryCauseLabels, err := localizedGroupLabels(tx, scope, "entry_cause", uiLang, "WHERE entry_cause IS NOT NULL", nil)
	if err != nil {
		return err
	}
	animalAgeLabels, err := localizedGroupLabels(tx, scope, "animal_age", uiLang, "WHERE animal_age IS NOT NULL", nil)
	if err != nil {
		return err
	}
	outtakeTypeLabels, err := localizedGroupLabels(tx, scope, "outtake_type", uiLang, "WHERE outtake_type IS NOT NULL", nil)
	if err != nil {
		return err
	}

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Set("pagination", q.Paginator)
		c.Set("animals", animals)
		c.Set("instances", instances)
		c.Set("speciesList", speciesList)
		c.Set("typesList", typesList)
		c.Set("citiesList", citiesList)
		c.Set("yearsList", yearsList)
		c.Set("entryCausesList", entryCausesList)
		c.Set("agesList", agesList)
		c.Set("outtakeTypesList", outtakeTypesList)
		c.Set("entryCauseLabels", entryCauseLabels)
		c.Set("animalAgeLabels", animalAgeLabels)
		c.Set("outtakeTypeLabels", outtakeTypeLabels)
		c.Set("viewMode", viewMode)
		return c.Render(http.StatusOK, r.HTML("consolidated_animals/index.plush.html"))
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(200, r.JSON(animals))
	}).Respond(c)
}

// animalsViewMode resolves the register view mode from params and scope.
func animalsViewMode(c buffalo.Context, scope ReportScope) string {
	viewMode := c.Param("view_mode")
	if viewMode == "" {
		viewMode = "global"
	}
	if !scope.IsGlobal() {
		viewMode = "instance"
	}
	return viewMode
}

// applyConsolidatedAnimalFilters adds WHERE clauses for the register search
// filters. entry_cause, animal_age and outtake_type match exactly on the
// stored canonical value; ring matches partially (LIKE).
func applyConsolidatedAnimalFilters(q *pop.Query, c buffalo.Context, viewMode string, scope ReportScope) *pop.Query {
	if viewMode == "instance" && scope.IsGlobal() {
		if instanceID := c.Param("instance_id"); instanceID != "" {
			q = q.Where("instance_id = ?", instanceID)
		}
	}
	if status := c.Param("status"); status != "" {
		q = q.Where("current_status = ?", status)
	}
	if species := c.Param("species"); species != "" {
		q = q.Where("species = ?", species)
	}
	if animalType := c.Param("animal_type"); animalType != "" {
		q = q.Where("animal_type = ?", animalType)
	}
	if city := c.Param("city"); city != "" {
		q = q.Where("discovery_city = ?", city)
	}
	if postalCode := c.Param("postal_code"); postalCode != "" {
		q = q.Where("discovery_postal_code = ?", postalCode)
	}
	if year := c.Param("year"); year != "" {
		q = q.Where("year = ?", year)
	}
	if entryCause := c.Param("entry_cause"); entryCause != "" {
		q = q.Where("entry_cause = ?", entryCause)
	}
	if animalAge := c.Param("animal_age"); animalAge != "" {
		q = q.Where("animal_age = ?", animalAge)
	}
	if ring := c.Param("ring"); ring != "" {
		q = q.Where("ring LIKE ?", "%"+ring+"%")
	}
	if outtakeType := c.Param("outtake_type"); outtakeType != "" {
		q = q.Where("outtake_type = ?", outtakeType)
	}
	return q
}

// consolidatedAnimalsCSVHeader returns the register export column headers in
// the request language (falls back to English).
func consolidatedAnimalsCSVHeader(lang string) []string {
	switch lang {
	case "fr":
		return []string{
			"Instance", "Année", "Numéro", "Espèce", "Type", "Âge", "Bague",
			"Date d'entrée", "Lieu de découverte", "Code postal", "Ville",
			"Cause d'entrée", "Statut", "Type de sortie", "Date de sortie", "Lieu de sortie",
		}
	case "de":
		return []string{
			"Instanz", "Jahr", "Nummer", "Art", "Typ", "Alter", "Ring",
			"Aufnahmedatum", "Fundort", "PLZ", "Stadt",
			"Fundursache", "Status", "Abgabeart", "Abgabedatum", "Abgabeort",
		}
	case "nl":
		return []string{
			"Instantie", "Jaar", "Nummer", "Soort", "Type", "Leeftijd", "Ring",
			"Opnamedatum", "Vindplaats", "Postcode", "Stad",
			"Vangstreden", "Status", "Afgifte type", "Afgiftedatum", "Afgifteplaats",
		}
	default:
		return []string{
			"Instance", "Year", "Number", "Species", "Type", "Age", "Ring",
			"Intake date", "Discovery location", "Postal code", "City",
			"Entry cause", "Status", "Outtake type", "Outtake date", "Outtake location",
		}
	}
}

func nullStr(s nulls.String) string {
	if s.Valid {
		return s.String
	}
	return ""
}

func formatDate(t nulls.Time) string {
	if t.Valid {
		return t.Time.Format("02/01/2006 15:04")
	}
	return ""
}

// ConsolidatedAnimalsExportCSV exports the consolidated animal register as a
// CSV file. It applies the same filters and report scope as
// ConsolidatedAnimalsIndex but no pagination.
func ConsolidatedAnimalsExportCSV(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}
	scope, err := reportScope(c, tx)
	if err != nil {
		return err
	}

	q := tx.Q()
	if !scope.IsGlobal() {
		q = q.Where("instance_id = ?", scope.InstanceID)
	}
	viewMode := animalsViewMode(c, scope)
	q = applyConsolidatedAnimalFilters(q, c, viewMode, scope)

	animals := &models.ConsolidatedAnimals{}
	if err := applyConsolidatedSort(q, c).All(animals); err != nil {
		return err
	}

	lang := requestUILang(c)
	header := consolidatedAnimalsCSVHeader(lang)
	rows := make([][]string, 0, len(*animals))
	for _, a := range *animals {
		rows = append(rows, []string{
			a.InstanceID,
			strconv.Itoa(a.Year),
			strconv.Itoa(a.YearNumber),
			a.LocalizedField(lang, "species"),
			a.LocalizedField(lang, "animal_type"),
			a.LocalizedField(lang, "animal_age"),
			nullStr(a.Ring),
			formatDate(a.IntakeDate),
			nullStr(a.DiscoveryLocation),
			nullStr(a.DiscoveryPostalCode),
			nullStr(a.DiscoveryCity),
			a.LocalizedField(lang, "entry_cause"),
			a.CurrentStatus,
			a.LocalizedField(lang, "outtake_type"),
			formatDate(a.OuttakeDate),
			nullStr(a.OuttakeLocation),
		})
	}

	return writeCSV(c, "consolidated_animals.csv", header, rows)
}

// ConsolidatedAnimalShow displays a single consolidated animal with drill-down
func ConsolidatedAnimalShow(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	animal := &models.ConsolidatedAnimal{}
	if err := tx.Find(animal, c.Param("consolidated_animal_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	// Get events for this animal
	events := &models.EventStreams{}
	if err := tx.Where("instance_id = ? AND animal_id = ?", animal.InstanceID, animal.AnimalID).Order("created_at asc").All(events); err != nil {
		return err
	}

	c.Set("animal", animal)
	c.Set("events", events)

	return responder.Wants("html", func(c buffalo.Context) error {
		return c.Render(http.StatusOK, r.HTML("consolidated_animals/show.plush.html"))
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(200, r.JSON(map[string]interface{}{
			"animal": animal,
			"events": events,
		}))
	}).Respond(c)
}

// ConsolidatedAnimalDrillDown shows detailed view with instance info
func ConsolidatedAnimalDrillDown(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	animal := &models.ConsolidatedAnimal{}
	if err := tx.Find(animal, c.Param("consolidated_animal_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	// Get all events for this animal
	events := &models.EventStreams{}
	if err := tx.Where("instance_id = ? AND animal_id = ?", animal.InstanceID, animal.AnimalID).Order("created_at asc").All(events); err != nil {
		return err
	}

	c.Set("animal", animal)
	c.Set("events", events)

	return c.Render(http.StatusOK, r.HTML("consolidated_animals/drill_down.plush.html"))
}

// Outcome-based classification (bug: reports must group deceased by
// positive/neutral/negative outtake outcome, not only by current_status).
//
// An animal counts as deceased (negative outcome) when its outtake rating is
// negative or the outtake is marked dead; animals that died in care without
// an outtake keep their current_status='died' classification. Released
// (non-negative outcome) covers positive and neutral ratings.
const sqlOutcomeDied = `(outtake_rating < 0 OR outtake_dead = 1 OR (current_status = 'died' AND outtake_rating IS NULL AND (outtake_dead IS NULL OR outtake_dead = 0)))`
const sqlOutcomeReleased = `((outtake_rating >= 0 AND (outtake_dead IS NULL OR outtake_dead = 0)) OR (current_status = 'released' AND outtake_rating IS NULL AND (outtake_dead IS NULL OR outtake_dead = 0)))`

// statusCount is one row of the current_status grouping.
type statusCount struct {
	Status string `db:"current_status"`
	Count  int    `db:"count"`
}

// tallyStatusCounts picks the well-known statuses out of a grouping.
func tallyStatusCounts(counts []statusCount) (inCare, released, died int) {
	for _, sc := range counts {
		switch sc.Status {
		case "in_care":
			inCare = sc.Count
		case "released":
			released = sc.Count
		case "died":
			died = sc.Count
		}
	}
	return inCare, released, died
}

// outcomeTally holds the outcome-based released/died counts plus the
// positive/neutral/negative breakdown.
type outcomeTally struct {
	Released int `db:"released"`
	Died     int `db:"died"`
	Positive int `db:"positive"`
	Neutral  int `db:"neutral"`
	Negative int `db:"negative"`
}

// tallyOutcomes computes outcome-based counts over the scoped register:
// released covers positive and neutral outtake outcomes, died covers
// negative outcomes plus animals that died in care without an outtake.
func tallyOutcomes(tx *pop.Connection, where string, args []interface{}) (outcomeTally, error) {
	var rows []outcomeTally
	// COALESCE guards the empty-register case: SUM over zero rows is NULL.
	err := tx.RawQuery(`SELECT
		COALESCE(SUM(CASE WHEN `+sqlOutcomeReleased+` THEN 1 ELSE 0 END), 0) as released,
		COALESCE(SUM(CASE WHEN `+sqlOutcomeDied+` THEN 1 ELSE 0 END), 0) as died,
		COALESCE(SUM(CASE WHEN outtake_rating > 0 AND (outtake_dead IS NULL OR outtake_dead = 0) THEN 1 ELSE 0 END), 0) as positive,
		COALESCE(SUM(CASE WHEN outtake_rating = 0 AND (outtake_dead IS NULL OR outtake_dead = 0) THEN 1 ELSE 0 END), 0) as neutral,
		COALESCE(SUM(CASE WHEN outtake_rating < 0 OR outtake_dead = 1 THEN 1 ELSE 0 END), 0) as negative
		FROM consolidated_animals `+where, args...).All(&rows)
	if len(rows) == 0 {
		return outcomeTally{}, err
	}
	return rows[0], err
}

// ReportsIndex shows the main reports dashboard
func ReportsIndex(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}
	scope, err := reportScope(c, tx)
	if err != nil {
		return err
	}
	where, args := ScopedWhere(scope, "")
	stats := make(map[string]interface{})
	var totalAnimals int
	if scope.IsGlobal() {
		totalAnimals, err = tx.Count(&models.ConsolidatedAnimal{})
	} else {
		totalAnimals, err = tx.Where("instance_id = ?", scope.InstanceID).Count(&models.ConsolidatedAnimal{})
	}
	if err != nil {
		return err
	}
	stats["total_animals"] = totalAnimals
	statusCounts := []statusCount{}
	if err = tx.RawQuery("SELECT current_status, COUNT(*) as count FROM consolidated_animals "+where+" GROUP BY current_status", args...).All(&statusCounts); err != nil {
		return err
	}
	stats["by_status"] = statusCounts
	inCare, _, _ := tallyStatusCounts(statusCounts)
	stats["in_care"] = inCare

	// Outcome-based released/died counts (positive/neutral vs negative
	// outtake outcome), with died-in-care fallback via current_status.
	outcome, err := tallyOutcomes(tx, where, args)
	if err != nil {
		return err
	}
	stats["released"] = outcome.Released
	stats["died"] = outcome.Died
	stats["outcome_positive"] = outcome.Positive
	stats["outcome_neutral"] = outcome.Neutral
	stats["outcome_negative"] = outcome.Negative
	yearCounts := []struct {
		Year  int `db:"year"`
		Count int `db:"count"`
	}{}
	if err = tx.RawQuery("SELECT year, COUNT(*) as count FROM consolidated_animals "+where+" GROUP BY year ORDER BY year DESC", args...).All(&yearCounts); err != nil {
		return err
	}
	stats["by_year"] = yearCounts
	speciesCounts := []struct {
		Species string `db:"species"`
		Count   int    `db:"count"`
	}{}
	speciesWhere, speciesArgs := ScopedWhere(scope, "WHERE species IS NOT NULL")
	if err = tx.RawQuery("SELECT species, COUNT(*) as count FROM consolidated_animals "+speciesWhere+" GROUP BY species ORDER BY count DESC LIMIT 20", speciesArgs...).All(&speciesCounts); err != nil {
		return err
	}
	stats["top_species"] = speciesCounts
	cityCounts := []struct {
		City  string `db:"city"`
		Count int    `db:"count"`
	}{}
	cityWhere, cityArgs := ScopedWhere(scope, "WHERE discovery_city IS NOT NULL")
	if err = tx.RawQuery("SELECT discovery_city as city, COUNT(*) as count FROM consolidated_animals "+cityWhere+" GROUP BY discovery_city ORDER BY count DESC LIMIT 20", cityArgs...).All(&cityCounts); err != nil {
		return err
	}
	stats["top_cities"] = cityCounts
	typeCounts := []struct {
		AnimalType string `db:"animal_type"`
		Count      int    `db:"count"`
	}{}
	typeWhere, typeArgs := ScopedWhere(scope, "WHERE animal_type IS NOT NULL")
	if err = tx.RawQuery("SELECT animal_type, COUNT(*) as count FROM consolidated_animals "+typeWhere+" GROUP BY animal_type ORDER BY count DESC", typeArgs...).All(&typeCounts); err != nil {
		return err
	}
	stats["by_type"] = typeCounts
	instances, err := listAnnualInstances(tx, scope)
	if err != nil {
		return err
	}
	c.Set("stats", stats)
	c.Set("instances", instances)
	c.Set("instanceID", scope.InstanceID)
	return c.Render(http.StatusOK, r.HTML("reports/index.plush.html"))
}

// ReportsByLocation shows animals grouped by discovery location
func ReportsByLocation(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}
	scope, err := reportScope(c, tx)
	if err != nil {
		return err
	}
	groupBy := c.Param("group_by")
	if groupBy == "" {
		groupBy = "city"
	}

	// Filter out rows missing the grouping column so grouped values are
	// never NULL (plain string scan targets).
	locationBase := "WHERE discovery_city IS NOT NULL"
	if groupBy == "postal_code" {
		locationBase = "WHERE discovery_postal_code IS NOT NULL"
	}
	whereLocation, locationArgs := ScopedWhere(scope, locationBase)

	var results []struct {
		Location   string `db:"location"`
		PostalCode string `db:"postal_code"`
		City       string `db:"city"`
		Count      int    `db:"count"`
		InCare     int    `db:"in_care"`
		Released   int    `db:"released"`
		Died       int    `db:"died"`
	}

	var query string
	if groupBy == "postal_code" {
		query = `SELECT 
			discovery_postal_code as location,
			discovery_postal_code as postal_code,
			COALESCE(MAX(discovery_city), '') as city,
			COUNT(*) as count,
			SUM(CASE WHEN current_status = 'in_care' THEN 1 ELSE 0 END) as in_care,
			SUM(CASE WHEN ` + sqlOutcomeReleased + ` THEN 1 ELSE 0 END) as released,
			SUM(CASE WHEN ` + sqlOutcomeDied + ` THEN 1 ELSE 0 END) as died
			FROM consolidated_animals 
			%s
			GROUP BY discovery_postal_code
			ORDER BY count DESC`
	} else {
		query = `SELECT 
			discovery_city as location,
			COALESCE(MAX(discovery_postal_code), '') as postal_code,
			discovery_city as city,
			COUNT(*) as count,
			SUM(CASE WHEN current_status = 'in_care' THEN 1 ELSE 0 END) as in_care,
			SUM(CASE WHEN ` + sqlOutcomeReleased + ` THEN 1 ELSE 0 END) as released,
			SUM(CASE WHEN ` + sqlOutcomeDied + ` THEN 1 ELSE 0 END) as died
			FROM consolidated_animals 
			%s
			GROUP BY discovery_city
			ORDER BY count DESC`
	}

	query = fmt.Sprintf(query, whereLocation)
	if err := tx.RawQuery(query, locationArgs...).All(&results); err != nil {
		return err
	}
	c.Set("results", results)
	c.Set("groupBy", groupBy)
	instances, err := listAnnualInstances(tx, scope)
	if err != nil {
		return err
	}
	c.Set("instances", instances)
	c.Set("instanceID", scope.InstanceID)
	return c.Render(http.StatusOK, r.HTML("reports/by_location.plush.html"))
}

func requestUILang(c buffalo.Context) string {
	if cookie, err := c.Request().Cookie("lang"); err == nil && cookie.Value != "" {
		return normalizeUILang(cookie.Value)
	}
	return "en-US"
}

func localizedGroupLabels(tx *pop.Connection, scope ReportScope, field, lang, baseWhere string, baseArgs []interface{}) (map[string]string, error) {
	var animals []models.ConsolidatedAnimal
	where, scopeArgs := ScopedWhere(scope, baseWhere)
	args := append(baseArgs, scopeArgs...)
	if err := tx.RawQuery("SELECT "+field+", translations FROM consolidated_animals "+where, args...).All(&animals); err != nil {
		return nil, err
	}
	labels := make(map[string]string)
	for _, animal := range animals {
		var canonical string
		switch field {
		case "animal_type":
			canonical = animal.AnimalType.String
		case "species":
			canonical = animal.Species.String
		}
		if canonical != "" {
			if _, exists := labels[canonical]; !exists {
				labels[canonical] = animal.LocalizedField(lang, field)
			}
		}
	}
	return labels, nil
}

func ReportsByType(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	scope, err := reportScope(c, tx)
	if err != nil {
		return err
	}
	whereType, typeArgs := ScopedWhere(scope, "WHERE animal_type IS NOT NULL")
	var results []struct {
		AnimalType string `db:"animal_type"`
		Count      int    `db:"count"`
		InCare     int    `db:"in_care"`
		Released   int    `db:"released"`
		Died       int    `db:"died"`
	}

	query := `SELECT 
		animal_type,
		COUNT(*) as count,
		SUM(CASE WHEN current_status = 'in_care' THEN 1 ELSE 0 END) as in_care,
		SUM(CASE WHEN ` + sqlOutcomeReleased + ` THEN 1 ELSE 0 END) as released,
		SUM(CASE WHEN ` + sqlOutcomeDied + ` THEN 1 ELSE 0 END) as died
		FROM consolidated_animals 
		%s
		GROUP BY animal_type
		ORDER BY count DESC`

	query = fmt.Sprintf(query, whereType)
	if err := tx.RawQuery(query, typeArgs...).All(&results); err != nil {
		return err
	}
	labels, err := localizedGroupLabels(tx, scope, "animal_type", requestUILang(c), "WHERE animal_type IS NOT NULL", nil)
	if err != nil {
		return err
	}
	c.Set("results", results)
	c.Set("localizedLabels", labels)
	instances, err := listAnnualInstances(tx, scope)
	if err != nil {
		return err
	}
	c.Set("instances", instances)
	c.Set("instanceID", scope.InstanceID)
	return c.Render(http.StatusOK, r.HTML("reports/by_type.plush.html"))
}

// ReportsBySpecies shows animals grouped by species
func ReportsBySpecies(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	year := c.Param("year")
	whereClause := "WHERE species IS NOT NULL"
	scope, err := reportScope(c, tx)
	if err != nil {
		return err
	}
	var labelArgs []interface{}
	var queryArgs []interface{}
	labelWhere := "WHERE species IS NOT NULL"
	if parsedYear, parseErr := strconv.Atoi(year); year != "" && parseErr == nil {
		whereClause = "WHERE species IS NOT NULL AND year = ?"
		labelWhere = "WHERE species IS NOT NULL AND year = ?"
		labelArgs = append(labelArgs, parsedYear)
	}
	whereClause, scopeArgs := ScopedWhere(scope, whereClause)
	queryArgs = append(queryArgs, labelArgs...)
	queryArgs = append(queryArgs, scopeArgs...)

	var results []struct {
		Species  string `db:"species"`
		Count    int    `db:"count"`
		InCare   int    `db:"in_care"`
		Released int    `db:"released"`
		Died     int    `db:"died"`
	}

	query := fmt.Sprintf(`SELECT 
		species,
		COUNT(*) as count,
		SUM(CASE WHEN current_status = 'in_care' THEN 1 ELSE 0 END) as in_care,
		SUM(CASE WHEN `+sqlOutcomeReleased+` THEN 1 ELSE 0 END) as released,
		SUM(CASE WHEN `+sqlOutcomeDied+` THEN 1 ELSE 0 END) as died
		FROM consolidated_animals 
		%s 
		GROUP BY species 
		ORDER BY count DESC`, whereClause)

	if err := tx.RawQuery(query, queryArgs...).All(&results); err != nil {
		return err
	}

	// Get available years for filter
	var yearsRows []struct {
		Year int `db:"year"`
	}
	yearWhere, yearArgs := ScopedWhere(scope, "")
	tx.RawQuery("SELECT DISTINCT year FROM consolidated_animals "+yearWhere+" ORDER BY year DESC", yearArgs...).All(&yearsRows)

	years := make([]yearOption, 0, len(yearsRows))
	for _, y := range yearsRows {
		years = append(years, yearOption{Year: y.Year, Selected: c.Param("year") == strconv.Itoa(y.Year)})
	}

	labels, err := localizedGroupLabels(tx, scope, "species", requestUILang(c), labelWhere, labelArgs)
	if err != nil {
		return err
	}
	c.Set("results", results)
	c.Set("years", years)
	c.Set("localizedLabels", labels)
	c.Set("selectedYear", year)
	instances, err := listAnnualInstances(tx, scope)
	if err != nil {
		return err
	}
	c.Set("instances", instances)
	c.Set("instanceID", scope.InstanceID)
	return c.Render(http.StatusOK, r.HTML("reports/by_species.plush.html"))
}
