package actions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"creaves-console/models"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
)

// archiveRootDir returns the directory under which event-deletion archives
// are written. Override with EVENT_ARCHIVE_DIR (tests, or deployments that
// want the archives outside the working directory).
func archiveRootDir() string {
	if dir := os.Getenv("EVENT_ARCHIVE_DIR"); dir != "" {
		return dir
	}
	return "archives"
}

// unsafe archive-name characters (path separators, traversal, control chars).
var archiveUnsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// sanitizeArchiveToken makes an instance_id safe to embed in an archive file
// name: it can never escape the archive directory.
func sanitizeArchiveToken(s string) string {
	token := archiveUnsafeChars.ReplaceAllString(strings.TrimSpace(s), "_")
	if token == "" {
		return "unknown"
	}
	return token
}

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
// instance_id). The events are serialized to a JSONL archive file before any
// DELETE runs; if archiving fails, nothing is removed.
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

	deleted, archivePath, err := archiveAndDeleteEvents(tx, scope, instanceID)
	if err != nil {
		return err
	}

	if deleted == 0 {
		c.Flash().Add("warning", "No events matched; nothing was deleted or archived")
	} else {
		c.Flash().Add("success", fmt.Sprintf("Deleted %d event(s); archive written to %s", deleted, archivePath))
	}
	return c.Redirect(http.StatusSeeOther, "/events")
}

// archiveAndDeleteEvents writes every event matched by the scope to a JSONL
// archive and then deletes exactly those rows. The archive is created first:
// a failure there leaves the database untouched.
func archiveAndDeleteEvents(tx *pop.Connection, scope, instanceID string) (deleted int, archivePath string, err error) {
	events := &models.EventStreams{}
	if scope == "instance" {
		err = tx.Where("instance_id = ?", instanceID).Order("imported_at asc").All(events)
	} else {
		err = tx.Order("imported_at asc").All(events)
	}
	if err != nil {
		return 0, "", err
	}

	archivePath, err = writeEventArchive(*events, scope, instanceID)
	if err != nil {
		return 0, "", err
	}

	if len(*events) == 0 {
		return 0, archivePath, nil
	}

	// Delete exactly the archived rows, by ID, in one transaction.
	ids := make([]string, 0, len(*events))
	for _, e := range *events {
		ids = append(ids, e.ID.String())
	}
	if err := tx.Transaction(func(t *pop.Connection) error {
		return t.RawQuery("DELETE FROM event_streams WHERE id IN (?)", ids).Exec()
	}); err != nil {
		return 0, "", err
	}
	return len(*events), archivePath, nil
}

// writeEventArchive serializes events to <archiveRootDir>/event-deletions/
// events-<utcstamp>-<scope>[-instance-<id>].jsonl, one full event JSON per
// line. An empty event set produces no file.
func writeEventArchive(events models.EventStreams, scope, instanceID string) (string, error) {
	if len(events) == 0 {
		return "", nil
	}
	name := fmt.Sprintf("events-%s-%s", time.Now().UTC().Format("20060102-150405"), scope)
	if scope == "instance" {
		name += "-instance-" + sanitizeArchiveToken(instanceID)
	}
	name += ".jsonl"

	dir := filepath.Join(archiveRootDir(), "event-deletions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("could not create archive directory: %w", err)
	}
	path := filepath.Join(dir, name)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("could not create archive file: %w", err)
	}
	defer f.Close()

	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			return "", fmt.Errorf("could not serialize event %s: %w", e.ID, err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return "", fmt.Errorf("could not write event %s to archive: %w", e.ID, err)
		}
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("could not flush archive file: %w", err)
	}
	return path, nil
}
