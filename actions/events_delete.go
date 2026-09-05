package actions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"creaves-console/models"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
)

// EventsDeleteNew renders the confirmation form for deleting received events
// (all of them, or only those from one instance).
func EventsDeleteNew(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	var instances []struct {
		InstanceID string `db:"instance_id"`
	}
	if err := tx.RawQuery("SELECT DISTINCT instance_id FROM event_streams ORDER BY instance_id").All(&instances); err != nil {
		return err
	}

	c.Set("instances", instances)
	return c.Render(http.StatusOK, r.HTML("events/delete.plush.html"))
}

// EventsDeleteCreate archives and then deletes received events. Scope:
//   - scope=all        → every event in the console
//   - scope=instance   → only the events of the given instance_id
//
// Both scopes require a typed confirmation ("DELETE ALL" resp. the exact
// instance_id). The events are archived in the event_stream_archives table
// (JSONL content) inside the same transaction as the DELETE; if archiving
// fails, nothing is removed. No files are written outside the database.
func EventsDeleteCreate(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	scope := strings.TrimSpace(c.Param("scope"))
	if scope == "" {
		scope = strings.TrimSpace(c.Request().FormValue("scope"))
	}
	instanceID := strings.TrimSpace(c.Param("instance_id"))
	if instanceID == "" {
		instanceID = strings.TrimSpace(c.Request().FormValue("instance_id"))
	}
	confirmation := strings.TrimSpace(c.Request().FormValue("confirmation"))

	switch scope {
	case "all":
		if confirmation != "DELETE ALL" {
			return c.Error(http.StatusUnprocessableEntity, fmt.Errorf("type exactly DELETE ALL to confirm deleting every event"))
		}
	case "instance":
		if instanceID == "" {
			return c.Error(http.StatusUnprocessableEntity, fmt.Errorf("instance_id is required when scope is instance"))
		}
		if confirmation != instanceID {
			return c.Error(http.StatusUnprocessableEntity, fmt.Errorf("type the exact instance_id to confirm deleting its events"))
		}
	default:
		return c.Error(http.StatusUnprocessableEntity, fmt.Errorf("scope must be all or instance"))
	}

	deleted, archiveID, err := archiveAndDeleteEvents(tx, scope, instanceID)
	if err != nil {
		return err
	}

	if deleted == 0 {
		c.Flash().Add("warning", "No events matched; nothing was deleted or archived")
	} else {
		c.Flash().Add("success", fmt.Sprintf("Deleted %d event(s); archive %s stored in the database", deleted, archiveID))
	}
	return c.Redirect(http.StatusSeeOther, "/events")
}

// archiveAndDeleteEvents archives every event matched by the scope into the
// event_stream_archives table (JSONL content) and then deletes exactly those
// rows — both inside one database transaction. A failure to archive rolls
// the deletion back and vice versa.
func archiveAndDeleteEvents(tx *pop.Connection, scope, instanceID string) (deleted int, archiveID string, err error) {
	events := &models.EventStreams{}
	if scope == "instance" {
		err = tx.Where("instance_id = ?", instanceID).Order("imported_at asc").All(events)
	} else {
		err = tx.Order("imported_at asc").All(events)
	}
	if err != nil {
		return 0, "", err
	}

	if len(*events) == 0 {
		return 0, "", nil
	}

	content, err := marshalEventsJSONL(*events)
	if err != nil {
		return 0, "", err
	}

	// Archive and delete atomically, in one transaction.
	err = tx.Transaction(func(t *pop.Connection) error {
		archive := &models.EventStreamArchive{
			Scope:      scope,
			InstanceID: instanceID,
			EventCount: len(*events),
			Content:    content,
		}
		if err := t.Create(archive); err != nil {
			return fmt.Errorf("could not store event archive: %w", err)
		}
		archiveID = archive.ID.String()

		ids := make([]string, 0, len(*events))
		for _, e := range *events {
			ids = append(ids, e.ID.String())
		}
		return t.RawQuery("DELETE FROM event_streams WHERE id IN (?)", ids).Exec()
	})
	if err != nil {
		return 0, "", err
	}
	return len(*events), archiveID, nil
}

// marshalEventsJSONL serializes events to the same JSONL payload the old file
// archive contained: one full event JSON per line.
func marshalEventsJSONL(events models.EventStreams) (string, error) {
	var buf bytes.Buffer
	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			return "", fmt.Errorf("could not serialize event %s: %w", e.ID, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return buf.String(), nil
}

// EventsArchivesIndex lists the event deletion archives stored in the
// database (admin only).
func EventsArchivesIndex(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	archives := &models.EventStreamArchives{}
	if err := tx.Order("created_at desc").All(archives); err != nil {
		return err
	}
	c.Set("archives", archives)
	return c.Render(http.StatusOK, r.HTML("events/archives.plush.html"))
}

// EventsArchiveDownload returns the JSONL content of one stored archive as an
// attachment (admin only).
func EventsArchiveDownload(c buffalo.Context) error {
	cu := GetCurrentUser(c)
	if cu == nil || !cu.Admin {
		return c.Error(http.StatusForbidden, fmt.Errorf("admin rights required"))
	}
	tx, ok := c.Value("tx").(*pop.Connection)
	if !ok {
		return fmt.Errorf("no transaction found")
	}

	archive := &models.EventStreamArchive{}
	if err := tx.Find(archive, c.Param("archive_id")); err != nil {
		return c.Error(http.StatusNotFound, err)
	}

	name := fmt.Sprintf("events-%s-%s", archive.CreatedAt.UTC().Format("20060102-150405"), archive.Scope)
	if archive.Scope == "instance" {
		name += "-instance-" + archive.InstanceID
	}
	name += ".jsonl"

	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	return c.Render(http.StatusOK, r.String(archive.Content))
}
