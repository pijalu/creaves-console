//go:build sqlite
// +build sqlite

package actions

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// listArchiveFiles returns every *.jsonl file under the test archive dir.
func listArchiveFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "event-deletions", "*.jsonl"))
	require.NoError(t, err)
	return matches
}

// readArchiveLines parses one JSONL archive file into event maps.
func readArchiveLines(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var lines []map[string]interface{}
	scanner := bufio.NewScanner(f)
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
	t.Setenv("EVENT_ARCHIVE_DIR", t.TempDir())
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
	t.Setenv("EVENT_ARCHIVE_DIR", t.TempDir())
	seedEventsFixtures(t, testDB)
	app := newEventsTestApp(testDB, false)

	rec := postEventsForm(t, app, "/events/delete", url.Values{
		"scope":        {"all"},
		"confirmation": {"DELETE ALL"},
	})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 4, countEvents(t, testDB, ""))
	assert.Empty(t, listArchiveFiles(t, os.Getenv("EVENT_ARCHIVE_DIR")))
}

func TestEventsDeleteRequiresExactConfirmation(t *testing.T) {
	t.Setenv("EVENT_ARCHIVE_DIR", t.TempDir())
	seedEventsFixtures(t, testDB)
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
	assert.Empty(t, listArchiveFiles(t, os.Getenv("EVENT_ARCHIVE_DIR")), "failed attempts must not write archives")
}

func TestEventsDeleteAllArchivesThenDeletes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVENT_ARCHIVE_DIR", dir)
	seedEventsFixtures(t, testDB)
	app := newEventsTestApp(testDB, true)

	rec := postEventsForm(t, app, "/events/delete", url.Values{
		"scope":        {"all"},
		"confirmation": {"DELETE ALL"},
	})
	require.Equal(t, http.StatusSeeOther, rec.Code)

	assert.Equal(t, 0, countEvents(t, testDB, ""))

	files := listArchiveFiles(t, dir)
	require.Len(t, files, 1)
	lines := readArchiveLines(t, files[0])
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
	dir := t.TempDir()
	t.Setenv("EVENT_ARCHIVE_DIR", dir)
	seedEventsFixtures(t, testDB)
	app := newEventsTestApp(testDB, true)

	rec := postEventsForm(t, app, "/events/delete", url.Values{
		"scope":        {"instance"},
		"instance_id":  {"center-a"},
		"confirmation": {"center-a"},
	})
	require.Equal(t, http.StatusSeeOther, rec.Code)

	assert.Equal(t, 0, countEvents(t, testDB, "center-a"))
	assert.Equal(t, 1, countEvents(t, testDB, "center-b"), "other instances must survive")

	files := listArchiveFiles(t, dir)
	require.Len(t, files, 1)
	assert.Contains(t, filepath.Base(files[0]), "center-a")
	lines := readArchiveLines(t, files[0])
	require.Len(t, lines, 3)
	for _, line := range lines {
		assert.Equal(t, "center-a", line["instance_id"])
	}
}

func TestEventsDeleteNothingMatchedWritesNoArchive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVENT_ARCHIVE_DIR", dir)
	seedEventsFixtures(t, testDB)
	require.NoError(t, testDB.RawQuery("DELETE FROM event_streams").Exec())
	app := newEventsTestApp(testDB, true)

	rec := postEventsForm(t, app, "/events/delete", url.Values{
		"scope":        {"all"},
		"confirmation": {"DELETE ALL"},
	})
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Empty(t, listArchiveFiles(t, dir), "an empty match must not create an archive file")
}

func TestSanitizeArchiveTokenBlocksTraversal(t *testing.T) {
	// Path separators must never survive: the token is embedded in a file
	// name, so "../" sequences would escape the archive directory.
	for _, raw := range []string{"../../etc/passwd", `..\..\evil`, "a/b", `c\d`} {
		sanitized := sanitizeArchiveToken(raw)
		assert.NotContains(t, sanitized, "/", raw)
		assert.NotContains(t, sanitized, "\\", raw)
	}
	assert.Equal(t, "center-a", sanitizeArchiveToken("center-a"))
	assert.Equal(t, "unknown", sanitizeArchiveToken("  "))
}

func TestArchiveFailureLeavesDatabaseUntouched(t *testing.T) {
	seedEventsFixtures(t, testDB)

	// An archive path that cannot be created (a file where the directory
	// should be) must abort the whole operation.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a dir"), 0o644))
	t.Setenv("EVENT_ARCHIVE_DIR", filepath.Join(blocker, "sub"))

	_, _, err := archiveAndDeleteEvents(testDB, "all", "")
	require.Error(t, err)
	assert.Equal(t, 4, countEvents(t, testDB, ""), "no deletion when archiving fails")
}
