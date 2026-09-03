package actions

import (
	"fmt"
	"net/http"

	"creaves-console/models"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
)

// instanceAnimalsRow couples a registered instance with the number of
// consolidated animals currently stored for it.
type instanceAnimalsRow struct {
	InstanceID  string
	Name        string
	AnimalCount int
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
		rows = append(rows, instanceAnimalsRow{
			InstanceID:  inst.InstanceID,
			Name:        inst.Name,
			AnimalCount: count,
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

	c.Flash().Add("success", fmt.Sprintf("Deleted all %d animals from the database", count))
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

	c.Flash().Add("success", fmt.Sprintf("Deleted %d animals of instance %s", count, instanceID))
	return c.Redirect(http.StatusSeeOther, "/sync_management")
}
