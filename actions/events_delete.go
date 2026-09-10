package actions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"creaves-console/models"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
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
	// Deleting events can precede a consolidated purge; drop cached
	// dropdown data so removed values cannot linger (refcache.go).
	refCacheInvalidateAll()

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
//
// Chunked keyset streaming: events are fetched in batches ordered by
// (imported_at, id), each batch is appended to the JSONL archive buffer and
// deleted immediately, so neither the event rows nor their payloads are ever
// fully materialized in memory (a full console wipe can involve 100k+ rows).
func archiveAndDeleteEvents(tx *pop.Connection, scope, instanceID string) (deleted int, archiveID string, err error) {
	const batchSize = 500

	// Cheap pre-check so an empty scope returns (0, "", nil) with no archive
	// row at all (preserved original behaviour).
	pre := tx.Q()
	if scope == "instance" {
		pre = pre.Where("instance_id = ?", instanceID)
	}
	empty, err := pre.Exists(&models.EventStream{})
	if err != nil {
		return 0, "", err
	}
	if !empty {
		return 0, "", nil
	}

	content := &bytes.Buffer{}
	archive := &models.EventStreamArchive{Scope: scope, InstanceID: instanceID}

	var cursorTime time.Time
	var cursorID uuid.UUID
	haveCursor := false

	err = tx.Transaction(func(t *pop.Connection) error {
		if err := t.Create(archive); err != nil {
			return fmt.Errorf("could not store event archive: %w", err)
		}
		archiveID = archive.ID.String()

		for {
			events := &models.EventStreams{}
			q := t.Q()
			if scope == "instance" {
				q = q.Where("instance_id = ?", instanceID)
			}
			if haveCursor {
				q = q.Where("(imported_at > ? OR (imported_at = ? AND id > ?))", cursorTime, cursorTime, cursorID)
			}
			if err := q.Order("imported_at asc, id asc").Limit(batchSize).All(events); err != nil {
				return err
			}
			if len(*events) == 0 {
				break
			}

			if err := appendEventsJSONL(content, *events); err != nil {
				return err
			}

			ids := make([]string, 0, len(*events))
			for _, e := range *events {
				ids = append(ids, e.ID.String())
			}
			last := &(*events)[len(*events)-1]
			cursorTime = last.ImportedAt
			cursorID = last.ID
			haveCursor = true

			if err := t.RawQuery("DELETE FROM event_streams WHERE id IN (?)", ids).Exec(); err != nil {
				return err
			}
			deleted += len(*events)

			if len(*events) < batchSize {
				break
			}
		}

		archive.EventCount = deleted
		archive.Content = content.String()
		if err := t.Update(archive); err != nil {
			return fmt.Errorf("could not store event archive content: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, "", err
	}
	return deleted, archiveID, nil
}

// appendEventsJSONL appends the given events to buf as JSONL: one full event
// JSON per line.
func appendEventsJSONL(buf *bytes.Buffer, events models.EventStreams) error {
	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("could not serialize event %s: %w", e.ID, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return nil
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
