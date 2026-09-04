package actions

import (
	"fmt"
	"net/http"

	"creaves-console/models"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
)

// instanceAnimalsRow couples a registered instance with the number of
// consolidated animals currently stored for it plus its sync status
// (expected/confirmed/unconfirmed record counts + checksums).
type instanceAnimalsRow struct {
	InstanceID  string
	Name        string
	AnimalCount int
	Status      *InstanceSyncStatus
}

// instanceYearRow groups the per-year stored-animal counts of one instance so
// a missing current-year cohort ("no animals of 2026") is visible at a glance.
type instanceYearRow struct {
	InstanceID string
	Name       string
	Years      []yearCount
}

type yearCount struct {
	Year  int
	Count int
}

// SyncManagementIndex renders the admin screen to manage synchronized
// animals: per-instance counts and global totals, with the destructive
// delete actions.
func SyncManagementIndex(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	instances := &models.CreavesInstances{}
	if err := tx.Order("instance_id").All(instances); err != nil {
		return err
	}

	rows := make([]instanceAnimalsRow, 0, len(*instances))
	total := 0
	for _, inst := range *instances {
		count, err := tx.Where("instance_id = ?", inst.InstanceID).Count(&models.ConsolidatedAnimal{})
		if err != nil {
			return err
		}
		status, err := ComputeInstanceSyncStatus(tx, inst.InstanceID)
		if err != nil {
			return err
		}
		rows = append(rows, instanceAnimalsRow{
			InstanceID:  inst.InstanceID,
			Name:        inst.Name,
			AnimalCount: count,
			Status:      status,
		})
		total += count
	}

	// Also count orphaned animals whose instance is no longer registered.
	var orphanCount int
	if err := tx.RawQuery(
		"SELECT COUNT(*) FROM consolidated_animals WHERE instance_id NOT IN (SELECT instance_id FROM creaves_instances)",
	).First(&orphanCount); err != nil {
		return err
	}

	c.Set("instanceRows", rows)
	c.Set("totalAnimals", total+orphanCount)
	c.Set("orphanAnimals", orphanCount)

	// Per-year stored counts per instance: a missing current-year cohort
	// (the historic "no animals of 2026" incident) must be visible here.
	type yearRow struct {
		InstanceID string `db:"instance_id"`
		Year       int    `db:"year"`
		Count      int    `db:"count"`
	}
	yearRows := []yearRow{}
	if err := tx.RawQuery(
		"SELECT instance_id, year, COUNT(*) as count FROM consolidated_animals GROUP BY instance_id, year ORDER BY instance_id asc, year desc",
	).All(&yearRows); err != nil {
		return err
	}
	byInstance := map[string]*instanceYearRow{}
	nameByID := map[string]string{}
	for _, r := range rows {
		nameByID[r.InstanceID] = r.Name
	}
	ordered := make([]*instanceYearRow, 0, len(rows))
	for _, y := range yearRows {
		group, ok := byInstance[y.InstanceID]
		if !ok {
			group = &instanceYearRow{InstanceID: y.InstanceID, Name: nameByID[y.InstanceID]}
			byInstance[y.InstanceID] = group
			ordered = append(ordered, group)
		}
		group.Years = append(group.Years, yearCount{Year: y.Year, Count: y.Count})
	}
	flat := make([]instanceYearRow, 0, len(ordered))
	for _, g := range ordered {
		flat = append(flat, *g)
	}
	c.Set("yearRows", flat)
	c.Set("hasYearRows", len(flat) > 0)
	return c.Render(http.StatusOK, r.HTML("sync_management/index.plush.html"))
}

// SyncManagementDeleteAllAnimals deletes ALL consolidated animals from the
// database (every instance). Events and instance registry are kept, so a
// full resync from Creaves rebuilds the data.
func SyncManagementDeleteAllAnimals(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	count, err := tx.Count(&models.ConsolidatedAnimal{})
	if err != nil {
		return err
	}

	if err := tx.RawQuery("DELETE FROM consolidated_animals").Exec(); err != nil {
		c.Flash().Add("danger", fmt.Sprintf("Failed to delete animals: %v", err))
		return c.Redirect(http.StatusSeeOther, "/sync_management")
	}

	// Re-queue the kept events so the consolidation runner rebuilds the
	// deleted rows. Without this reset the events stay "processed" forever:
	// ProcessUnprocessedEvents skips them and webhook redelivery is deduped
	// by event ID, so those animals would never reappear (the historic
	// missing-year incident).
	if err := tx.RawQuery("UPDATE event_streams SET processed_at = NULL").Exec(); err != nil {
		c.Flash().Add("danger", fmt.Sprintf("Failed to re-queue events for rebuild: %v", err))
		return c.Redirect(http.StatusSeeOther, "/sync_management")
	}

	c.Flash().Add("success", fmt.Sprintf("Deleted all %d animals from the database; events re-queued for consolidation", count))
	return c.Redirect(http.StatusSeeOther, "/sync_management")
}

// SyncManagementDeleteInstanceAnimals deletes all consolidated animals of a
// single instance (form param "instance_id"). Events and registry are kept,
// so a full resync from that Creaves instance rebuilds the data.
func SyncManagementDeleteInstanceAnimals(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}

	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	instanceID := c.Param("instance_id")
	if instanceID == "" {
		c.Flash().Add("danger", "No instance selected")
		return c.Redirect(http.StatusSeeOther, "/sync_management")
	}

	count, err := tx.Where("instance_id = ?", instanceID).Count(&models.ConsolidatedAnimal{})
	if err != nil {
		return err
	}

	if err := tx.RawQuery("DELETE FROM consolidated_animals WHERE instance_id = ?", instanceID).Exec(); err != nil {
		c.Flash().Add("danger", fmt.Sprintf("Failed to delete animals for instance %s: %v", instanceID, err))
		return c.Redirect(http.StatusSeeOther, "/sync_management")
	}

	// Re-queue this instance's kept events so the consolidation runner
	// rebuilds the deleted rows (same rationale as delete-all: a processed
	// event whose consolidated row is gone would otherwise never be
	// reprocessed — redelivery is deduped by event ID).
	if err := tx.RawQuery("UPDATE event_streams SET processed_at = NULL WHERE instance_id = ?", instanceID).Exec(); err != nil {
		c.Flash().Add("danger", fmt.Sprintf("Failed to re-queue events for rebuild: %v", err))
		return c.Redirect(http.StatusSeeOther, "/sync_management")
	}

	c.Flash().Add("success", fmt.Sprintf("Deleted %d animals of instance %s; events re-queued for consolidation", count, instanceID))
	return c.Redirect(http.StatusSeeOther, "/sync_management")
}
