//go:build sqlite
// +build sqlite

package actions

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"creaves-console/models"
	"github.com/gobuffalo/pop/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// postEventsForm POSTs a form to the test app and returns the recorder.
func postEventsForm(t *testing.T, app interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

// countEvents returns the number of event_streams rows, optionally filtered
// by instance.
func countEvents(t *testing.T, tx *pop.Connection, instance string) int {
	t.Helper()
	var count int
	var err error
	if instance == "" {
		count, err = tx.Count(&models.EventStream{})
	} else {
		count, err = tx.Where("instance_id = ?", instance).Count(&models.EventStream{})
	}
	require.NoError(t, err)
	return count
}

// listArchives returns every event_stream_archives row, newest first.
func listArchives(t *testing.T, tx *pop.Connection) models.EventStreamArchives {
	t.Helper()
	archives := models.EventStreamArchives{}
	require.NoError(t, tx.Order("created_at desc").All(&archives))
	return archives
}

// countArchives returns the number of event_stream_archives rows.
func countArchives(t *testing.T, tx *pop.Connection) int {
	t.Helper()
	count, err := tx.Count(&models.EventStreamArchive{})
	require.NoError(t, err)
	return count
}

// resetArchives empties the archive table.
func resetArchives(t *testing.T, tx *pop.Connection) {
	t.Helper()
	require.NoError(t, tx.RawQuery("DELETE FROM event_stream_archives").Exec())
}

// parseArchiveJSONL parses the JSONL content of one stored archive into
// event maps; every line must be valid JSON.
func parseArchiveJSONL(t *testing.T, content string) []map[string]interface{} {
	t.Helper()
	var lines []map[string]interface{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(line), &m), "archive line must be valid JSON: %s", line)
		lines = append(lines, m)
	}
	require.NoError(t, scanner.Err())
	return lines
}

func TestEventsDeleteNewForbiddenForNonAdmin(t *testing.T) {
	app := newEventsTestApp(testDB, false)
	rec := getEventsPage(t, app, "/events/delete")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestEventsDeleteNewRendersFormForAdmin(t *testing.T) {
	seedEventsFixtures(t, testDB)
	app := newEventsTestApp(testDB, true)

	// The static /events/delete route must win over /events/{event_id}
	// (EventShow would answer 404 for the "delete" segment).
	rec := getEventsPage(t, app, "/events/delete")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `action="/events/delete"`)
	assert.Contains(t, rec.Body.String(), `name="scope"`)
	assert.Contains(t, rec.Body.String(), `name="confirmation"`)
}

func TestEventsDeleteForbiddenForNonAdmin(t *testing.T) {
	seedEventsFixtures(t, testDB)
	resetArchives(t, testDB)
	app := newEventsTestApp(testDB, false)

	rec := postEventsForm(t, app, "/events/delete", url.Values{
		"scope":        {"all"},
		"confirmation": {"DELETE ALL"},
	})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 4, countEvents(t, testDB, ""))
	assert.Equal(t, 0, countArchives(t, testDB))
}

func TestEventsDeleteRequiresExactConfirmation(t *testing.T) {
	seedEventsFixtures(t, testDB)
	resetArchives(t, testDB)
	app := newEventsTestApp(testDB, true)

	for _, form := range []url.Values{
		{"scope": {"all"}, "confirmation": {"delete all"}},
		{"scope": {"all"}, "confirmation": {""}},
		{"scope": {"instance"}, "instance_id": {"center-a"}, "confirmation": {"center-b"}},
		{"scope": {"instance"}, "instance_id": {""}, "confirmation": {""}},
		{"scope": {"bogus"}, "confirmation": {"anything"}},
	} {
		rec := postEventsForm(t, app, "/events/delete", form)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "form: %v", form)
	}
	assert.Equal(t, 4, countEvents(t, testDB, ""), "no event may be deleted without exact confirmation")
	assert.Equal(t, 3, countEvents(t, testDB, "center-a"))
	assert.Equal(t, 0, countArchives(t, testDB), "failed attempts must not write archives")
}

func TestEventsDeleteAllArchivesThenDeletes(t *testing.T) {
	seedEventsFixtures(t, testDB)
	resetArchives(t, testDB)
	app := newEventsTestApp(testDB, true)

	rec := postEventsForm(t, app, "/events/delete", url.Values{
		"scope":        {"all"},
		"confirmation": {"DELETE ALL"},
	})
	require.Equal(t, http.StatusSeeOther, rec.Code)

	assert.Equal(t, 0, countEvents(t, testDB, ""))

	archives := listArchives(t, testDB)
	require.Len(t, archives, 1)
	archive := archives[0]
	assert.Equal(t, "all", archive.Scope)
	assert.Equal(t, "", archive.InstanceID)
	assert.Equal(t, 4, archive.EventCount)

	lines := parseArchiveJSONL(t, archive.Content)
	require.Len(t, lines, 4)

	// Every archived line must carry the full original event data.
	instanceIDs := map[string]bool{}
	for _, line := range lines {
		assert.Contains(t, line, "id")
		assert.Contains(t, line, "payload")
		assert.Contains(t, line, "imported_at")
		instanceIDs[line["instance_id"].(string)] = true
	}
	assert.Equal(t, map[string]bool{"center-a": true, "center-b": true}, instanceIDs)
}

func TestEventsDeleteInstanceScopeOnlyTouchesThatInstance(t *testing.T) {
	seedEventsFixtures(t, testDB)
	resetArchives(t, testDB)
	app := newEventsTestApp(testDB, true)

	rec := postEventsForm(t, app, "/events/delete", url.Values{
		"scope":        {"instance"},
		"instance_id":  {"center-a"},
		"confirmation": {"center-a"},
	})
	require.Equal(t, http.StatusSeeOther, rec.Code)

	assert.Equal(t, 0, countEvents(t, testDB, "center-a"))
	assert.Equal(t, 1, countEvents(t, testDB, "center-b"), "other instances must survive")

	archives := listArchives(t, testDB)
	require.Len(t, archives, 1)
	archive := archives[0]
	assert.Equal(t, "instance", archive.Scope)
	assert.Equal(t, "center-a", archive.InstanceID)
	assert.Equal(t, 3, archive.EventCount)

	lines := parseArchiveJSONL(t, archive.Content)
	require.Len(t, lines, 3)
	for _, line := range lines {
		assert.Equal(t, "center-a", line["instance_id"])
	}
}

func TestEventsDeleteNothingMatchedWritesNoArchive(t *testing.T) {
	seedEventsFixtures(t, testDB)
	resetArchives(t, testDB)
	require.NoError(t, testDB.RawQuery("DELETE FROM event_streams").Exec())
	app := newEventsTestApp(testDB, true)

	rec := postEventsForm(t, app, "/events/delete", url.Values{
		"scope":        {"all"},
		"confirmation": {"DELETE ALL"},
	})
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, 0, countArchives(t, testDB), "an empty match must not create an archive row")
}

func TestArchiveAndDeleteIsAtomic(t *testing.T) {
	seedEventsFixtures(t, testDB)
	resetArchives(t, testDB)

	// Success-path invariant: after one call, archived rows and deleted rows
	// match exactly (4 archived, 0 left).
	deleted, archiveID, err := archiveAndDeleteEvents(testDB, "all", "")
	require.NoError(t, err)
	assert.Equal(t, 4, deleted)
	require.NotEmpty(t, archiveID)

	archives := listArchives(t, testDB)
	require.Len(t, archives, 1)
	assert.Equal(t, archiveID, archives[0].ID.String())
	assert.Equal(t, 4, archives[0].EventCount)
	assert.Equal(t, 0, countEvents(t, testDB, ""))
}

func TestArchiveFailureLeavesDatabaseUntouched(t *testing.T) {
	// Scratch SQLite DB WITHOUT the event_stream_archives table: archiving
	// must fail and the transaction must roll back, leaving events in place.
	db, err := pop.NewConnection(&pop.ConnectionDetails{
		Dialect:  "sqlite",
		Database: ":memory:",
	})
	require.NoError(t, err)
	require.NoError(t, db.Open())
	defer db.Close()

	require.NoError(t, db.RawQuery(`
		CREATE TABLE event_streams (
			id TEXT PRIMARY KEY,
			instance_id TEXT NOT NULL,
			animal_id INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			payload TEXT,
			source_db TEXT NOT NULL DEFAULT '',
			resync_run_id TEXT,
			imported_at TIMESTAMP NOT NULL,
			processed_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`).Exec())

	e := &models.EventStream{
		InstanceID: "center-x", AnimalID: 1, EventType: models.EventTypeAnimalDiscovered,
		Payload: []byte(`{}`), ImportedAt: time.Now().UTC(),
	}
	require.NoError(t, db.Create(e))

	_, _, err = archiveAndDeleteEvents(db, "all", "")
	require.Error(t, err, "archiving into a missing table must fail")
	assert.Equal(t, 1, countEvents(t, db, ""), "no deletion when archiving fails")
}

func TestEventsArchivesIndexForbiddenForNonAdmin(t *testing.T) {
	app := newEventsTestApp(testDB, false)
	rec := getEventsPage(t, app, "/events/archives")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestEventsArchivesIndexAndDownload(t *testing.T) {
	seedEventsFixtures(t, testDB)
	resetArchives(t, testDB)
	app := newEventsTestApp(testDB, true)

	// Create one archive through the real delete flow.
	rec := postEventsForm(t, app, "/events/delete", url.Values{
		"scope":        {"instance"},
		"instance_id":  {"center-a"},
		"confirmation": {"center-a"},
	})
	require.Equal(t, http.StatusSeeOther, rec.Code)

	archives := listArchives(t, testDB)
	require.Len(t, archives, 1)
	archive := archives[0]

	// Index page lists the archive.
	rec = getEventsPage(t, app, "/events/archives")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), archive.ID.String())

	// Download returns the stored JSONL as an attachment.
	rec = getEventsPage(t, app, "/events/archives/"+archive.ID.String()+"/download")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "center-a")
	assert.Equal(t, archive.Content, rec.Body.String())
	lines := parseArchiveJSONL(t, rec.Body.String())
	assert.Len(t, lines, 3)
}

func TestEventsArchiveDownloadUnknownID(t *testing.T) {
	app := newEventsTestApp(testDB, true)
	rec := getEventsPage(t, app, "/events/archives/00000000-0000-0000-0000-000000000000/download")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
