package actions

import (
	"creaves-console/models"
	"fmt"
	"net/http"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gobuffalo/x/responder"
)

// DashboardIndex displays the main dashboard for the consolidation app
func DashboardIndex(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	// Get statistics
	stats := make(map[string]interface{})

	// Total animals
	totalAnimals, err := tx.Count(&models.ConsolidatedAnimal{})
	if err != nil {
		return err
	}
	stats["total_animals"] = totalAnimals

	// By status
	statusCounts := []struct {
		Status string `db:"current_status"`
		Count  int    `db:"count"`
	}{}
	if err := tx.RawQuery("SELECT current_status, COUNT(*) as count FROM consolidated_animals GROUP BY current_status").All(&statusCounts); err != nil {
		return err
	}
	statusMap := make(map[string]int)
	for _, sc := range statusCounts {
		statusMap[sc.Status] = sc.Count
	}
	stats["by_status"] = statusMap

	// By instance
	instanceCounts := []struct {
		InstanceID string `db:"instance_id"`
		Count      int    `db:"count"`
	}{}
	if err := tx.RawQuery("SELECT instance_id, COUNT(*) as count FROM consolidated_animals GROUP BY instance_id").All(&instanceCounts); err != nil {
		return err
	}
	instanceMap := make(map[string]int)
	for _, ic := range instanceCounts {
		instanceMap[ic.InstanceID] = ic.Count
	}
	stats["by_instance"] = instanceMap

	// Total events
	totalEvents, err := tx.Count(&models.EventStream{})
	if err != nil {
		return err
	}
	stats["total_events"] = totalEvents

	// Unprocessed events
	unprocessedEvents, err := tx.Where("processed_at IS NULL").Count(&models.EventStream{})
	if err != nil {
		return err
	}
	stats["unprocessed_events"] = unprocessedEvents

	// Unique instances (from event_streams)
	var uniqueInstances int
	if err := tx.RawQuery("SELECT COUNT(DISTINCT instance_id) FROM event_streams").First(&uniqueInstances); err != nil {
		return err
	}
	stats["unique_instances"] = uniqueInstances

	// Total webhook API keys
	totalKeys, err := tx.Count(&models.WebhookAPIKey{})
	if err != nil {
		return err
	}
	stats["total_webhook_keys"] = totalKeys

	activeKeys, err := tx.Where("active = ?", true).Count(&models.WebhookAPIKey{})
	if err != nil {
		return err
	}
	stats["active_webhook_keys"] = activeKeys

	c.Set("stats", stats)

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

	// Apply filters
	viewMode := c.Param("view_mode")
	if viewMode == "" {
		viewMode = "global"
	}
	if viewMode == "instance" {
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

	if err := q.Order("year desc, year_number asc").All(animals); err != nil {
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

	var yearsList []struct {
		Year int `db:"year"`
	}
	tx.RawQuery("SELECT DISTINCT year FROM consolidated_animals ORDER BY year DESC").All(&yearsList)

	return responder.Wants("html", func(c buffalo.Context) error {
		c.Set("pagination", q.Paginator)
		c.Set("animals", animals)
		c.Set("instances", instances)
		c.Set("speciesList", speciesList)
		c.Set("typesList", typesList)
		c.Set("citiesList", citiesList)
		c.Set("yearsList", yearsList)
		c.Set("viewMode", viewMode)
		return c.Render(http.StatusOK, r.HTML("consolidated_animals/index.plush.html"))
	}).Wants("json", func(c buffalo.Context) error {
		return c.Render(200, r.JSON(animals))
	}).Respond(c)
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

// ReportsIndex shows the main reports dashboard
func ReportsIndex(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	stats := make(map[string]interface{})

	// Total animals
	totalAnimals, err := tx.Count(&models.ConsolidatedAnimal{})
	if err != nil {
		return err
	}
	stats["total_animals"] = totalAnimals

	// By status
	statusCounts := []struct {
		Status string `db:"current_status"`
		Count  int    `db:"count"`
	}{}
	if err := tx.RawQuery("SELECT current_status, COUNT(*) as count FROM consolidated_animals GROUP BY current_status").All(&statusCounts); err != nil {
		return err
	}
	stats["by_status"] = statusCounts

	// By year
	yearCounts := []struct {
		Year  int `db:"year"`
		Count int `db:"count"`
	}{}
	if err := tx.RawQuery("SELECT year, COUNT(*) as count FROM consolidated_animals GROUP BY year ORDER BY year DESC").All(&yearCounts); err != nil {
		return err
	}
	stats["by_year"] = yearCounts

	// Top species
	speciesCounts := []struct {
		Species string `db:"species"`
		Count   int    `db:"count"`
	}{}
	if err := tx.RawQuery("SELECT species, COUNT(*) as count FROM consolidated_animals WHERE species IS NOT NULL GROUP BY species ORDER BY count DESC LIMIT 20").All(&speciesCounts); err != nil {
		return err
	}
	stats["top_species"] = speciesCounts

	// Top cities
	cityCounts := []struct {
		City  string `db:"city"`
		Count int    `db:"count"`
	}{}
	if err := tx.RawQuery("SELECT discovery_city as city, COUNT(*) as count FROM consolidated_animals WHERE discovery_city IS NOT NULL GROUP BY discovery_city ORDER BY count DESC LIMIT 20").All(&cityCounts); err != nil {
		return err
	}
	stats["top_cities"] = cityCounts

	// Top types
	typeCounts := []struct {
		AnimalType string `db:"animal_type"`
		Count      int    `db:"count"`
	}{}
	if err := tx.RawQuery("SELECT animal_type, COUNT(*) as count FROM consolidated_animals WHERE animal_type IS NOT NULL GROUP BY animal_type ORDER BY count DESC").All(&typeCounts); err != nil {
		return err
	}
	stats["by_type"] = typeCounts

	c.Set("stats", stats)
	return c.Render(http.StatusOK, r.HTML("reports/index.plush.html"))
}

// ReportsByLocation shows animals grouped by discovery location
func ReportsByLocation(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	groupBy := c.Param("group_by")
	if groupBy == "" {
		groupBy = "city"
	}

	var results []struct {
		Location     string `db:"location"`
		PostalCode   string `db:"postal_code"`
		City         string `db:"city"`
		Count        int    `db:"count"`
		InCare       int    `db:"in_care"`
		Released     int    `db:"released"`
		Died         int    `db:"died"`
	}

	var query string
	if groupBy == "postal_code" {
		query = `SELECT 
			discovery_postal_code as location,
			discovery_postal_code as postal_code,
			MAX(discovery_city) as city,
			COUNT(*) as count,
			SUM(CASE WHEN current_status = 'in_care' THEN 1 ELSE 0 END) as in_care,
			SUM(CASE WHEN current_status = 'released' THEN 1 ELSE 0 END) as released,
			SUM(CASE WHEN current_status = 'died' THEN 1 ELSE 0 END) as died
			FROM consolidated_animals 
			WHERE discovery_postal_code IS NOT NULL 
			GROUP BY discovery_postal_code 
			ORDER BY count DESC`
	} else {
		query = `SELECT 
			discovery_city as location,
			MAX(discovery_postal_code) as postal_code,
			discovery_city as city,
			COUNT(*) as count,
			SUM(CASE WHEN current_status = 'in_care' THEN 1 ELSE 0 END) as in_care,
			SUM(CASE WHEN current_status = 'released' THEN 1 ELSE 0 END) as released,
			SUM(CASE WHEN current_status = 'died' THEN 1 ELSE 0 END) as died
			FROM consolidated_animals 
			WHERE discovery_city IS NOT NULL 
			GROUP BY discovery_city 
			ORDER BY count DESC`
	}

	if err := tx.RawQuery(query).All(&results); err != nil {
		return err
	}

	c.Set("results", results)
	c.Set("groupBy", groupBy)
	return c.Render(http.StatusOK, r.HTML("reports/by_location.plush.html"))
}

// ReportsByType shows animals grouped by type
func ReportsByType(c buffalo.Context) error {
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

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
		SUM(CASE WHEN current_status = 'released' THEN 1 ELSE 0 END) as released,
		SUM(CASE WHEN current_status = 'died' THEN 1 ELSE 0 END) as died
		FROM consolidated_animals 
		WHERE animal_type IS NOT NULL 
		GROUP BY animal_type 
		ORDER BY count DESC`

	if err := tx.RawQuery(query).All(&results); err != nil {
		return err
	}

	c.Set("results", results)
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
	if year != "" {
		whereClause = fmt.Sprintf("WHERE species IS NOT NULL AND year = %s", year)
	}

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
		SUM(CASE WHEN current_status = 'released' THEN 1 ELSE 0 END) as released,
		SUM(CASE WHEN current_status = 'died' THEN 1 ELSE 0 END) as died
		FROM consolidated_animals 
		%s 
		GROUP BY species 
		ORDER BY count DESC`, whereClause)

	if err := tx.RawQuery(query).All(&results); err != nil {
		return err
	}

	// Get available years for filter
	var years []struct {
		Year int `db:"year"`
	}
	tx.RawQuery("SELECT DISTINCT year FROM consolidated_animals ORDER BY year DESC").All(&years)

	c.Set("results", results)
	c.Set("years", years)
	c.Set("selectedYear", year)
	return c.Render(http.StatusOK, r.HTML("reports/by_species.plush.html"))
}
