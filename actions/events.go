package actions

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"creaves-console/models"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
)

// EventsIndex lists the webhook events received from the source instances.
// It is a read-only diagnostic view: operators use it to verify what the
// console actually received and whether each event made it into the
// consolidated view.
func EventsIndex(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	events := &models.EventStreams{}
	q := tx.PaginateFromParams(c.Params())

	if instanceID := c.Param("instance_id"); instanceID != "" {
		q = q.Where("instance_id = ?", instanceID)
	}
	if eventType := c.Param("event_type"); eventType != "" {
		q = q.Where("event_type = ?", eventType)
	}
	switch c.Param("processed") {
	case "processed":
		q = q.Where("processed_at IS NOT NULL")
	case "pending":
		q = q.Where("processed_at IS NULL")
	}

	if err := q.Order("imported_at desc").All(events); err != nil {
		return err
	}

	var instances []struct {
		InstanceID string `db:"instance_id"`
	}
	if err := tx.RawQuery("SELECT DISTINCT instance_id FROM event_streams ORDER BY instance_id").All(&instances); err != nil {
		return err
	}

	c.Set("events", events)
	c.Set("instances", instances)
	c.Set("pagination", q.Paginator)
	c.Set("eventTypes", models.EventTypes)
	return c.Render(http.StatusOK, r.HTML("events/index.plush.html"))
}

// EventShow renders one received event with its full payload as
// pretty-printed JSON, and links to the consolidated animal when present.
func EventShow(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	event := &models.EventStream{}
	if err := tx.Find(event, c.Param("event_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	var pretty bytes.Buffer
	if len(event.Payload) > 0 {
		if err := json.Indent(&pretty, event.Payload, "", "  "); err != nil {
			// Fall back to the raw payload rather than failing the page.
			pretty.Reset()
			pretty.Write(event.Payload)
		}
	}

	var consolidated struct {
		ID uuid.UUID `db:"id"`
	}
	err := tx.RawQuery(
		"SELECT id FROM consolidated_animals WHERE instance_id = ? AND animal_id = ?",
		event.InstanceID, event.AnimalID).First(&consolidated)
	consolidatedPath := ""
	if err == nil {
		consolidatedPath = "/consolidated_animals/" + consolidated.ID.String()
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	c.Set("event", event)
	c.Set("payload", pretty.String())
	c.Set("consolidatedPath", consolidatedPath)
	return c.Render(http.StatusOK, r.HTML("events/show.plush.html"))
}
