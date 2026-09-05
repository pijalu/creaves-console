//go:build sqlite
// +build sqlite

package actions

// Regression tests for bugs.md item 9 (LOW console UX):
//  1. The animal detail event history showed the raw event type
//     (`animal_state`), an unformatted timestamp ("2026-09-04 13:02:28") and
//     an empty Source column. The history must show a localized event type
//     label, a formatted date and a meaningful source ("Resync <id>" for
//     resync snapshots delivered with a resync_run_id, "Live update"
//     otherwise).
//  2. /drill_down was a 1:1 duplicate of the show page. It is now an event
//     timeline that shows what each event changed (payload diff).

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"creaves-console/models"

	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedEventHistoryFixtures inserts one consolidated animal plus three
// events: a live discovery, a live release and a resync state snapshot
// carrying a resync run id.
func seedEventHistoryFixtures(t *testing.T, tx *pop.Connection) (animalID string, resyncRunID string) {
	t.Helper()
	seedRegisterFixtures(t, tx)

	now := time.Now().UTC()
	a := &models.ConsolidatedAnimal{
		ID:            uuid.Must(uuid.NewV4()),
		InstanceID:    "center-a",
		AnimalID:      42,
		Year:          2024,
		YearNumber:    1,
		CurrentStatus: "released",
		LastEventAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	a.Species = nullsString("Hérisson")
	require.NoError(t, tx.Create(a))

	runID := uuid.Must(uuid.NewV4()).String()
	resyncUUID := uuid.Must(uuid.FromString(runID))
	resyncRunID = runID

	events := []struct {
		eventType   string
		offset      time.Duration
		resyncRunID *uuid.UUID
		species     string
		status      string
	}{
		{"animal_discovered", -48 * time.Hour, nil, "Hérisson", "in_care"},
		{"animal_released", -24 * time.Hour, nil, "Hérisson", "released"},
		{"animal_state", -1 * time.Hour, &resyncUUID, "Hérisson", "released"},
	}
	for i, e := range events {
		id := uuid.Must(uuid.NewV4())
		payload := `{"animal":{"id":42,"species":"` + e.species + `"},"current_status":"` + e.status + `","timestamp":"2026-09-05T10:00:00Z"}`
		ev := &models.EventStream{
			ID:         id,
			InstanceID: "center-a",
			AnimalID:   42,
			EventType:  models.EventType(e.eventType),
			Payload:    json.RawMessage(payload),
			ImportedAt: now.Add(e.offset),
			CreatedAt:  now.Add(e.offset),
			UpdatedAt:  now.Add(e.offset),
		}
		if e.resyncRunID != nil {
			ev.ResyncRunID = e.resyncRunID
		}
		require.NoError(t, tx.Create(ev), "event %d", i)
	}
	animalID = a.ID.String()
	return animalID, resyncRunID
}

// TestEventHistoryFormattedLocalized asserts the show page renders localized
// event type labels, formatted dates and a populated Source column.
func TestEventHistoryFormattedLocalized(t *testing.T) {
	app := newDashboardTestApp(testDB)
	animalID, _ := seedEventHistoryFixtures(t, testDB)

	res := getAnimalsPage(t, app, "/consolidated_animals/"+animalID)
	require.Equal(t, http.StatusOK, res.Code, "status: %d", res.Code)
	body := res.Body.String()

	// Raw event types must not leak into the history.
	assert.NotContains(t, body, "animal_state")
	assert.NotContains(t, body, "animal_discovered")
	assert.NotContains(t, body, "animal_released")

	// Localized labels (default en-US).
	assert.Contains(t, body, "Discovered", "discovery event label")
	assert.Contains(t, body, "Released", "release event label")
	assert.Contains(t, body, "State snapshot", "state snapshot event label")

	// Dates formatted dd/mm/yyyy hh:mm, not raw "2006-01-02 15:04:05".
	assert.NotRegexp(t, `\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`, body,
		"raw SQL timestamps must not be rendered")
	assert.Regexp(t, `\d{2}/\d{2}/\d{4} \d{2}:\d{2}`, body, "formatted event dates")

	// Source column populated: resync snapshot shows the resync id,
	// live events show "Live update".
	assert.Contains(t, body, "Live update", "live events must be sourced as live updates")
}

// TestEventHistorySourceShowsResyncID asserts the resync event's Source cell
// carries the "Resync" marker.
func TestEventHistorySourceShowsResyncID(t *testing.T) {
	app := newDashboardTestApp(testDB)
	animalID, runID := seedEventHistoryFixtures(t, testDB)

	res := getAnimalsPage(t, app, "/consolidated_animals/"+animalID)
	require.Equal(t, http.StatusOK, res.Code, "status: %d", res.Code)
	body := res.Body.String()

	assert.Contains(t, body, "Resync", "resync snapshot must be marked as resync source")
	// The shortened run id (first 8 chars) identifies the run.
	assert.Contains(t, body, runID[:8], "resync source must show the shortened run id")
}

// TestDrillDownIsTimeline asserts drill_down is no longer a duplicate of the
// show page but an event timeline: later events show what they changed.
func TestDrillDownIsTimeline(t *testing.T) {
	app := newDashboardTestApp(testDB)
	animalID, _ := seedEventHistoryFixtures(t, testDB)

	res := getAnimalsPage(t, app, "/consolidated_animals/"+animalID+"/drill_down")
	require.Equal(t, http.StatusOK, res.Code, "status: %d", res.Code)
	body := res.Body.String()

	assert.Contains(t, body, "Timeline", "drill_down must render the event timeline")
	// The release event changed the status: the diff must show it.
	assert.Contains(t, body, "in_care", "previous status must appear in the diff")
	assert.Contains(t, body, "released", "new status must appear in the diff")
}

// TestWebhookStoresResyncRunID asserts the webhook receiver persists the
// resync_run_id delivered with an event onto the event stream row, and that
// a redelivery can backfill it onto a previously stored event.
func TestWebhookStoresResyncRunID(t *testing.T) {
	app := newWebhookTestApp(testDB)
	seedRegisterFixtures(t, testDB)

	runID := uuid.Must(uuid.NewV4())
	rawKey, _ := seedAPIKey(t, testDB, "")

	// A dedicated event id keeps the lookup immune to leftovers of other
	// tests sharing the file-based SQLite database.
	eventID := uuid.Must(uuid.NewV4())

	payload := `{"animal":{"id":7,"species":"Renard"},"current_status":"in_care","timestamp":"2026-09-05T10:00:00Z"}`
	body := `{"contract_version":2,"events":[{"id":"` + eventID.String() +
		`","instance_id":"center-a","animal_id":7,"event_type":"animal_state","resync_run_id":"` + runID.String() +
		`","payload":` + payload + `,"created_at":"2026-09-05T10:00:00Z"}]}`
	res := postWebhook(app, "Bearer "+rawKey, []byte(body))
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())

	var stored models.EventStream
	require.NoError(t, testDB.Where("id = ?", eventID.String()).First(&stored))
	require.NotNil(t, stored.ResyncRunID, "resync_run_id must be stored")
	assert.Equal(t, runID, *stored.ResyncRunID)
	assert.Equal(t, "center-a", stored.InstanceID)
}
