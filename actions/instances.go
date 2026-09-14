package actions

import (
	"fmt"
	"net/http"
	"strings"

	"creaves-console/models"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
)

// purgeInstance atomically removes all console data owned by one source.
func purgeInstance(tx *pop.Connection, instanceID string) error {
	return withTx(tx, func(t *pop.Connection) error {
		if err := t.Where("instance_id = ?", instanceID).Delete(&models.EventStream{}); err != nil {
			return err
		}
		if err := t.Where("instance_id = ?", instanceID).Delete(&models.ConsolidatedAnimal{}); err != nil {
			return err
		}
		if err := t.Where("instance_id = ?", instanceID).Delete(&models.WebhookAPIKey{}); err != nil {
			return err
		}
		if err := t.Where("instance_id = ?", instanceID).Delete(&models.CreavesInstance{}); err != nil {
			return err
		}
		return nil
	})
}

type instanceAdminView struct {
	models.CreavesInstance
	AnimalCount int
	EventCount  int
	KeyCount    int
	Keys        models.WebhookAPIKeys
}

func loadInstanceAdminView(tx *pop.Connection, instanceID string) (*instanceAdminView, error) {
	instance := &models.CreavesInstance{}
	// Exact match first (index-backed); fall back to the case-insensitive
	// lookup only for hand-typed URLs.
	err := tx.Where("instance_id = ?", instanceID).First(instance)
	if err != nil {
		if err := tx.Where("LOWER(instance_id) = LOWER(?)", instanceID).First(instance); err != nil {
			return nil, err
		}
	}
	canonicalID := instance.InstanceID
	animals, err := CountConsolidatedAnimals(tx, canonicalID)
	if err != nil {
		return nil, err
	}
	events, err := CountEventStreams(tx, canonicalID)
	if err != nil {
		return nil, err
	}
	keys := &models.WebhookAPIKeys{}
	if err := tx.Where("instance_id = ?", canonicalID).Order("name asc").All(keys); err != nil {
		return nil, err
	}
	return &instanceAdminView{CreavesInstance: *instance, AnimalCount: animals, EventCount: events, KeyCount: len(*keys), Keys: *keys}, nil
}

func InstanceShow(c buffalo.Context) error {
	user := GetCurrentUser(c)
	if user == nil || !user.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}
	view, err := loadInstanceAdminView(tx, c.Param("instance_id"))
	if err != nil {
		return c.Error(http.StatusNotFound, err)
	}
	c.Set("instance", view)
	return c.Render(http.StatusOK, r.HTML("instances/show.plush.html"))
}

func InstancesIndex(c buffalo.Context) error {
	user := GetCurrentUser(c)
	if user == nil || !user.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}
	instances := &models.CreavesInstances{}
	if err := tx.All(instances); err != nil {
		return err
	}
	// Two GROUP BY queries for all instances instead of one count query per
	// instance (N+1): the instance list page scales with the register size.
	type countRow struct {
		InstanceID string `db:"instance_id"`
		Count      int    `db:"count"`
	}
	animalCounts := []countRow{}
	if err := tx.RawQuery("SELECT instance_id, COUNT(*) as count FROM consolidated_animals GROUP BY instance_id").All(&animalCounts); err != nil {
		return err
	}
	eventCounts := []countRow{}
	if err := tx.RawQuery("SELECT instance_id, COUNT(*) as count FROM event_streams GROUP BY instance_id").All(&eventCounts); err != nil {
		return err
	}
	agg := make(map[string]*instanceAdminView, len(*instances))
	views := make([]instanceAdminView, 0, len(*instances))
	for _, instance := range *instances {
		keys := &models.WebhookAPIKeys{}
		if err := tx.Where("instance_id = ?", instance.InstanceID).Order("name asc").All(keys); err != nil {
			return err
		}
		views = append(views, instanceAdminView{CreavesInstance: instance, KeyCount: len(*keys), Keys: *keys})
		agg[instance.InstanceID] = &views[len(views)-1]
	}
	for _, r := range animalCounts {
		if v, ok := agg[r.InstanceID]; ok {
			v.AnimalCount = r.Count
		}
	}
	for _, r := range eventCounts {
		if v, ok := agg[r.InstanceID]; ok {
			v.EventCount = r.Count
		}
	}
	c.Set("instances", views)
	return c.Render(http.StatusOK, r.HTML("instances/index.plush.html"))
}

func InstanceCleanup(c buffalo.Context) error {
	user := GetCurrentUser(c)
	if user == nil || !user.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}
	id := c.Param("instance_id")
	instance := &models.CreavesInstance{}
	if err := tx.Where("instance_id = ?", id).First(instance); err != nil {
		return c.Error(http.StatusNotFound, err)
	}
	confirmation := strings.TrimSpace(c.Param("instance_id_confirmation"))
	if confirmation == "" {
		confirmation = strings.TrimSpace(c.Request().FormValue("instance_id_confirmation"))
	}
	if confirmation != id {
		return c.Error(http.StatusUnprocessableEntity, fmt.Errorf("type the exact instance_id to confirm cleanup"))
	}
	if err := purgeInstance(tx, id); err != nil {
		return err
	}
	// Purged rows can remove the last carriers of cached dropdown values.
	refCacheInvalidateAll()
	dataCacheInvalidateAll()
	c.Flash().Add("success", "Instance cleaned; trigger a full resync from Creaves")
	return c.Redirect(http.StatusSeeOther, "/instances")
}
