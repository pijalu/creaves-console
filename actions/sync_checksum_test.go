//go:build sqlite
// +build sqlite

package actions

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"time"

	"creaves-console/models"
	"creaves-console/templates"

	"github.com/gobuffalo/nulls"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stateHashPayload builds an animal_state payload JSON with the given hash.
func stateHashPayload(t *testing.T, hash string) []byte {
	t.Helper()
	return []byte(`{"animal":{"id":1},"state_hash":"` + hash + `"}`)
}

// seedSyncChecksumFixtures builds, for center-a:
//   - animal 1: state event (hash h1) + consolidated row with hash h1 → confirmed
//   - animal 2: state event (hash h2) + consolidated row with a STALE hash → unconfirmed (mismatch)
//   - animal 3: only a discovered event (no state event, no row) → unconfirmed (missing)
//   - animal 4: consolidated row with NO state hash → ignored for confirmation
//   - animal 5: state event with an OLDER created_at + newer hash (latest wins)
//
// center-b gets one unrelated event (scoping check).
func seedSyncChecksumFixtures(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.RawQuery("DELETE FROM consolidated_animals").Exec())
	require.NoError(t, testDB.RawQuery("DELETE FROM event_streams").Exec())
	require.NoError(t, testDB.RawQuery("DELETE FROM creaves_instances").Exec())

	now := time.Now().UTC()
	inst := &models.CreavesInstance{ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", Name: "center-a", FirstSeenAt: now, LastSeenAt: now}
	require.NoError(t, testDB.Create(inst))

	stateEvent := func(animalID int, hash string, at time.Time) {
		e := &models.EventStream{
			ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: animalID,
			EventType: models.EventTypeAnimalState, Payload: stateHashPayload(t, hash),
			ImportedAt: at, CreatedAt: at,
		}
		require.NoError(t, testDB.Create(e))
	}

	stateEvent(1, "h1", now.Add(-3*time.Hour))
	stateEvent(2, "h2", now.Add(-3*time.Hour))
	stateEvent(5, "h5-old", now.Add(-5*time.Hour))
	stateEvent(5, "h5-new", now.Add(-1*time.Hour))
	require.NoError(t, testDB.Create(&models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: 3,
		EventType: models.EventTypeAnimalDiscovered, Payload: []byte(`{}`),
		ImportedAt: now, CreatedAt: now,
	}))
	require.NoError(t, testDB.Create(&models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-b", AnimalID: 99,
		EventType: models.EventTypeAnimalDiscovered, Payload: []byte(`{}`),
		ImportedAt: now, CreatedAt: now,
	}))

	row := func(animalID, year int, hash string) {
		a := &models.ConsolidatedAnimal{
			ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a",
			AnimalID: animalID, Year: year, YearNumber: 1,
			Species: nulls.NewString("Hérisson"),
		}
		if hash != "" {
			a.StateHash = nulls.NewString(hash)
		}
		require.NoError(t, testDB.Create(a))
	}
	row(1, 2025, "h1")       // confirmed
	row(2, 2026, "h2-stale") // mismatch
	row(4, 2024, "")         // no state hash
	row(5, 2026, "h5-new")   // confirmed via latest event
}

func TestStateSetChecksum_GoldenAndDeterminism(t *testing.T) {
	// Empty input: SHA-256 of empty string.
	assert.Equal(t,
		"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		StateSetChecksum(nil))

	lines := []string{"2|aaa", "1|bbb", "10|ccc"}
	got := StateSetChecksum(lines)
	// Sorted lexicographically: "1|bbb", "10|ccc", "2|aaa".
	assert.Equal(t, got, StateSetChecksum([]string{"1|bbb", "10|ccc", "2|aaa"}))
	assert.True(t, strings.HasPrefix(got, "sha256:"), "checksum must be prefixed: %s", got)
	assert.NotEqual(t, got, StateSetChecksum([]string{"2|aaa", "1|bbb"}), "different set must differ")
}

func TestInstanceSyncStatus_CountsAndChecksums(t *testing.T) {
	seedSyncChecksumFixtures(t)

	status, err := ComputeInstanceSyncStatus(testDB, "center-a")
	require.NoError(t, err)

	// Expected = distinct animals seen in ANY event for the instance: 1,2,3,5.
	assert.Equal(t, 4, status.ExpectedTotal)
	// Confirmed: animal 1 (h1==h1) and animal 5 (latest event h5-new == row).
	assert.Equal(t, 2, status.Confirmed)
	// Unconfirmed: animal 2 (stale row hash) and animal 3 (no row at all).
	assert.Equal(t, 2, status.Unconfirmed)

	// Event-log checksum covers the latest state hash of animals 1,2,5.
	assert.Equal(t,
		StateSetChecksum([]string{"1|h1", "2|h2", "5|h5-new"}),
		status.EventLogChecksum)
	// Consolidated checksum covers rows with a state hash: 1,2,5.
	assert.Equal(t,
		StateSetChecksum([]string{"1|h1", "2|h2-stale", "5|h5-new"}),
		status.ConsolidatedChecksum)
	assert.NotEqual(t, status.EventLogChecksum, status.ConsolidatedChecksum,
		"stale row must make the two checksums differ")
}

func TestInstanceSyncStatus_EmptyInstance(t *testing.T) {
	seedSyncChecksumFixtures(t)

	status, err := ComputeInstanceSyncStatus(testDB, "missing")
	require.NoError(t, err)
	assert.Equal(t, 0, status.ExpectedTotal)
	assert.Equal(t, 0, status.Confirmed)
	assert.Equal(t, 0, status.Unconfirmed)
	// No data: checksums stay EMPTY (never the sha256 of an empty string,
	// the historic false "checksum match") and NoData is flagged so the UI
	// shows a no-data state that can never count as a match.
	assert.Empty(t, status.EventLogChecksum)
	assert.Empty(t, status.ConsolidatedChecksum)
	assert.True(t, status.NoData)
	assert.False(t, status.ChecksumsMatch())
}

// TestInstanceSyncStatus_AnnouncedStatusLoaded proves the console loads the
// producer-announced expected sync state (stored on the instance row) and
// compares its stored-animal checksum against it: only non-empty equality
// counts as a match.
func TestInstanceSyncStatus_AnnouncedStatusLoaded(t *testing.T) {
	seedSyncChecksumFixtures(t)

	// No announcement yet: not stored, nothing matches, nothing claimed.
	status, err := ComputeInstanceSyncStatus(testDB, "center-a")
	require.NoError(t, err)
	assert.False(t, status.HasAnnouncement())
	assert.Nil(t, status.AnnouncedExpectedTotal)
	assert.Empty(t, status.AnnouncedExpectedChecksum)
	assert.False(t, status.ChecksumMatchesAnnounced())

	// Announce a DIFFERENT expected state: loaded, but no match.
	now := time.Now().UTC()
	require.NoError(t, models.StoreAnnouncedSyncStatus(testDB, "center-a", 10, "sha256:producer10", now))
	status, err = ComputeInstanceSyncStatus(testDB, "center-a")
	require.NoError(t, err)
	require.NotNil(t, status.AnnouncedExpectedTotal)
	assert.Equal(t, 10, *status.AnnouncedExpectedTotal)
	assert.Equal(t, "sha256:producer10", status.AnnouncedExpectedChecksum)
	require.NotNil(t, status.AnnouncedAt)
	assert.True(t, status.HasAnnouncement())
	assert.False(t, status.ChecksumMatchesAnnounced(), "different announced checksum must warn, never pass")

	// Announce the actual stored checksum: match.
	require.NoError(t, models.StoreAnnouncedSyncStatus(testDB, "center-a", 4, status.ConsolidatedChecksum, now))
	status, err = ComputeInstanceSyncStatus(testDB, "center-a")
	require.NoError(t, err)
	assert.True(t, status.ChecksumMatchesAnnounced(), "equal non-empty checksums must match")
}

// TestWebhookEventsHandler_ConfirmsStateHashesAndStoresAnnouncement is the
// console half of the ack round-trip: the webhook response confirms, per
// processed animal_state event, the state hash it stored; and a "sync"
// envelope block is persisted on the instance row as the announced expected
// total/checksum.
func TestWebhookEventsHandler_ConfirmsStateHashesAndStoresAnnouncement(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")

	announcedAt := time.Now().UTC().Add(-time.Hour)
	event := WebhookEvent{
		ID:         uuid.Must(uuid.NewV4()).String(),
		InstanceID: "inst-1",
		AnimalID:   77,
		EventType:  string(models.EventTypeAnimalState),
		Payload:    []byte(`{"animal":{"id":77,"species":"Hedgehog","year":2025,"year_number":3},"current_status":"in_care","state_hash":"hash-77"}`),
		CreatedAt:  time.Now(),
	}
	payload, _ := json.Marshal(WebhookPayload{
		ContractVersion: 2,
		Instance:        &InstanceInfo{ID: "inst-1", Name: "Center One"},
		Sync:            &SyncAnnouncement{ExpectedTotal: 42, ExpectedChecksum: "sha256:announced42", AnnouncedAt: &announcedAt},
		Events:          []WebhookEvent{event},
	})
	rec := postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Processed     int `json:"processed"`
		ConfirmedList []struct {
			ID        string `json:"id"`
			StateHash string `json:"state_hash"`
		} `json:"confirmed"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.EqualValues(t, 1, resp.Processed)
	require.Len(t, resp.ConfirmedList, 1, "processed animal_state event must be confirmed")
	assert.Equal(t, event.ID, resp.ConfirmedList[0].ID)
	assert.Equal(t, "hash-77", resp.ConfirmedList[0].StateHash)

	// The announcement is stored on the instance row.
	inst := &models.CreavesInstance{}
	require.NoError(t, tx.Where("instance_id = ?", "inst-1").First(inst))
	assert.True(t, inst.AnnouncedExpectedTotal.Valid)
	assert.Equal(t, 42, inst.AnnouncedExpectedTotal.Int)
	assert.True(t, inst.AnnouncedExpectedChecksum.Valid)
	assert.Equal(t, "sha256:announced42", inst.AnnouncedExpectedChecksum.String)
	assert.True(t, inst.AnnouncedAt.Valid)

	// Non-state events are never confirmed: only the animal_state event is.
	discovered := WebhookEvent{
		ID: uuid.Must(uuid.NewV4()).String(), InstanceID: "inst-1", AnimalID: 78,
		EventType: string(models.EventTypeAnimalDiscovered), Payload: []byte(`{"animal":{"id":78}}`),
		CreatedAt: time.Now(),
	}
	payload, _ = json.Marshal(WebhookPayload{Events: []WebhookEvent{discovered}})
	rec = postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)
	resp = struct {
		Processed     int `json:"processed"`
		ConfirmedList []struct {
			ID        string `json:"id"`
			StateHash string `json:"state_hash"`
		} `json:"confirmed"`
	}{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.ConfirmedList, "non animal_state events must not be confirmed")
}

// TestWebhookEventsHandler_RefreshesLegacyPayloadOnRedelivery pins the
// legacy backfill path: a force resync re-sends a legacy event (whose stored
// payload predates state_hash) with the state_hash added. The console must
// adopt the refreshed payload, re-apply it and acknowledge the delivery —
// otherwise the producer can never confirm legacy animals.
func TestWebhookEventsHandler_RefreshesLegacyPayloadOnRedelivery(t *testing.T) {
	tx := setupTest(t)
	app := newWebhookTestApp(tx)
	rawKey, _ := seedAPIKey(t, tx, "")

	eventID := uuid.Must(uuid.NewV4()).String()
	legacy := WebhookEvent{
		ID: eventID, InstanceID: "inst-1", AnimalID: 79,
		EventType: string(models.EventTypeAnimalState),
		Payload:   []byte(`{"animal":{"id":79,"species":"Hedgehog","year":2025,"year_number":3},"current_status":"in_care"}`),
		CreatedAt: time.Now(),
	}
	payload, _ := json.Marshal(WebhookPayload{Events: []WebhookEvent{legacy}})
	rec := postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)

	var first struct {
		ConfirmedList []struct {
			ID string `json:"id"`
		} `json:"confirmed"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &first))
	assert.Empty(t, first.ConfirmedList, "a legacy payload without state_hash cannot be confirmed")

	// Redelivery of the SAME deterministic event with state_hash backfilled.
	refreshed := legacy
	refreshed.Payload = []byte(`{"animal":{"id":79,"species":"Hedgehog","year":2025,"year_number":3},"current_status":"in_care","state_hash":"hash-79"}`)
	payload, _ = json.Marshal(WebhookPayload{Events: []WebhookEvent{refreshed}})
	rec = postWebhook(app, "Bearer "+rawKey, payload)
	require.Equal(t, http.StatusOK, rec.Code)

	var second struct {
		Processed     int `json:"processed"`
		ConfirmedList []struct {
			ID        string `json:"id"`
			StateHash string `json:"state_hash"`
		} `json:"confirmed"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &second))
	assert.EqualValues(t, 1, second.Processed)
	require.Len(t, second.ConfirmedList, 1, "the refreshed event must be acknowledged")
	assert.Equal(t, eventID, second.ConfirmedList[0].ID)
	assert.Equal(t, "hash-79", second.ConfirmedList[0].StateHash)

	// The stored event and the consolidated snapshot carry the state hash.
	stored := &models.EventStream{}
	require.NoError(t, tx.Find(stored, eventID))
	p, err := stored.GetPayload()
	require.NoError(t, err)
	assert.Equal(t, "hash-79", p.StateHash)
	row := &models.ConsolidatedAnimal{}
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", "inst-1", 79).First(row))
	require.True(t, row.StateHash.Valid)
	assert.Equal(t, "hash-79", row.StateHash.String)
}

// TestSyncManagementIndex_ShowsProducerBadges validates the admin UI: the
// announced expected total renders, a matching producer checksum earns the
// "matches producer" badge, an unannounced/empty instance shows "no data".
func TestSyncManagementIndex_ShowsProducerBadges(t *testing.T) {
	seedSyncChecksumFixtures(t)

	status, err := ComputeInstanceSyncStatus(testDB, "center-a")
	require.NoError(t, err)
	require.NoError(t, models.StoreAnnouncedSyncStatus(testDB, "center-a", 99, status.ConsolidatedChecksum, time.Now().UTC()))

	// center-b is registered but has no state events and no consolidated
	// rows: its checksum cell must show the no-data state.
	require.NoError(t, testDB.Create(&models.CreavesInstance{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-b", Name: "center-b",
		FirstSeenAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(),
	}))

	app := newSyncManagementTestApp(testDB, true)
	rec := perform(app, http.MethodGet, "/sync_management")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Contains(t, body, ">99<", "announced expected total must render")
	assert.Contains(t, body, "matches producer", "equal non-empty checksums must earn the producer badge")
	assert.Contains(t, body, "no data yet", "an instance without any state data must show the no-data state")
}

func TestSyncManagementIndex_ShowsSyncColumns(t *testing.T) {
	seedSyncChecksumFixtures(t)
	app := newSyncManagementTestApp(testDB, true)

	rec := perform(app, http.MethodGet, "/sync_management")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Contains(t, body, "center-a")
	// Expected/confirmed/unconfirmed numbers for center-a must be rendered.
	assert.Contains(t, body, ">4</", "expected total must render")
	assert.Contains(t, body, ">2</", "confirmed and unconfirmed counts must render")
	// Checksums must be shown (truncated) for cross-app comparison.
	assert.Contains(t, body, "sha256:", "checksum must render")
}

func TestSyncManagementIndex_YearBreakdown(t *testing.T) {
	seedSyncChecksumFixtures(t)
	app := newSyncManagementTestApp(testDB, true)

	rec := perform(app, http.MethodGet, "/sync_management")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	// Per-year stored counts must be visible so a missing current year
	// ("no animals of 2026") is immediately detectable.
	assert.Contains(t, body, "2026")
	assert.Contains(t, body, "2025")
	assert.Contains(t, body, "2024")
}

func TestSyncManagementLocaleVariantsShowSyncColumns(t *testing.T) {
	for _, locale := range []string{"", ".fr", ".de", ".nl"} {
		path := "sync_management/index.plush" + locale + ".html"
		body, err := fs.ReadFile(templates.FS(), path)
		require.NoError(t, err, "locale template %s must be embedded", path)
		assert.Contains(t, string(body), "row.Status.ExpectedTotal", "%s must render expected count", path)
		assert.Contains(t, string(body), "row.Status.ChecksumsMatch()", "%s must render checksum badge", path)
		assert.Contains(t, string(body), "row.Status.ChecksumMatchesAnnounced()", "%s must render the producer-comparison badge", path)
		assert.Contains(t, string(body), "row.Status.HasAnnouncement()", "%s must gate the announced expected total", path)
		assert.Contains(t, string(body), "row.Status.AnnouncedExpectedTotal", "%s must render the announced expected total", path)
		assert.Contains(t, string(body), "row.Status.NoData", "%s must render the no-data state", path)
		assert.Contains(t, string(body), "yearRows", "%s must render per-year breakdown", path)
	}
}
