//go:build sqlite
// +build sqlite

package actions

import (
	"testing"
	"time"

	"creaves-console/models"

	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BUG-8 regression: the console applies webhook events synchronously in
// receipt order. A full redelivery (console cleanup + force resync, or sync
// target recreate) arrives in arbitrary batch order, so years-old historical
// events used to overwrite newer states — the rebuilt view was
// non-deterministic (died 5,775 → 2,152 in the observed scramble).
//
// The per-animal ordering guard makes application order-insensitive: events
// strictly OLDER than the newest event time applied for the animal are
// recorded as processed but not applied. These tests replay one animal's
// lifecycle in several permutations and pin the same, correct final state.

// orderEvent is one lifecycle event of the test animal with its logical
// (payload) timestamp; CreatedAt mirrors it like the wire does.
type orderEvent struct {
	label string
	ev    *models.EventStream
	at    time.Time
}

const orderInstance = "center-n"
const orderAnimal = 2058

func mustTs(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return ts
}

// orderLifecycleEvents builds the acceptance sequence for one animal:
// discovered → state(died) → released → state(died). The final consolidated
// state must always be the LATEST state event (died with the Décédé outtake).
func orderLifecycleEvents(t *testing.T) []orderEvent {
	t.Helper()
	return []orderEvent{
		{
			label: "discovered",
			at:    mustTs(t, "2024-05-01T08:00:00Z"),
			ev: &models.EventStream{
				InstanceID: orderInstance, AnimalID: orderAnimal,
				EventType: models.EventTypeAnimalDiscovered,
				Payload:   []byte(`{"animal":{"id":2058,"species":"Renard"},"current_status":"in_care","timestamp":"2024-05-01T08:00:00Z"}`),
			},
		},
		{
			label: "state-died-1",
			at:    mustTs(t, "2024-06-01T09:00:00Z"),
			ev: &models.EventStream{
				InstanceID: orderInstance, AnimalID: orderAnimal,
				EventType: models.EventTypeAnimalState,
				Payload:   []byte(`{"animal":{"id":2058,"species":"Renard","cage":"C1"},"current_status":"died","outtake":{"type":"Décédé","rating":-1,"dead":true,"error":false},"state_hash":"hash-died-1","timestamp":"2024-06-01T09:00:00Z"}`),
			},
		},
		{
			label: "released",
			at:    mustTs(t, "2024-07-01T10:00:00Z"),
			ev: &models.EventStream{
				InstanceID: orderInstance, AnimalID: orderAnimal,
				EventType: models.EventTypeAnimalReleased,
				Payload:   []byte(`{"animal":{"id":2058},"current_status":"released","outtake":{"type":"Relâché","rating":1,"dead":false,"error":false},"timestamp":"2024-07-01T10:00:00Z"}`),
			},
		},
		{
			label: "state-died-2",
			at:    mustTs(t, "2024-08-01T11:00:00Z"),
			ev: &models.EventStream{
				InstanceID: orderInstance, AnimalID: orderAnimal,
				EventType: models.EventTypeAnimalState,
				Payload:   []byte(`{"animal":{"id":2058,"species":"Renard","cage":"C2"},"current_status":"died","outtake":{"type":"Décédé","rating":-1,"dead":true,"error":false},"state_hash":"hash-died-2","timestamp":"2024-08-01T11:00:00Z"}`),
			},
		},
	}
}

// processInOrder semantics live directly in the tests below: each test seeds
// all events, then calls processor.processEvent in a chosen delivery order.

func TestEventProcessor_OrderInsensitiveFinalState(t *testing.T) {
	// Deterministic permutations of the 4-event lifecycle, including exact
	// reverse (worst case: newest first) and interleavings.
	permutations := [][]int{
		{0, 1, 2, 3}, // chronological
		{3, 2, 1, 0}, // exact reverse
		{1, 3, 0, 2},
		{2, 0, 3, 1},
		{3, 0, 2, 1},
		{1, 0, 3, 2},
	}

	for _, perm := range permutations {
		perm := perm
		t.Run(permLabel(perm), func(t *testing.T) {
			tx := setupTest(t)
			defs := orderLifecycleEvents(t)

			events := make([]*models.EventStream, len(defs))
			for i, def := range defs {
				ev := *def.ev
				ev.ID = uuid.Must(uuid.NewV4())
				ev.CreatedAt = def.at
				require.NoError(t, tx.Create(&ev))
				events[i] = &ev
			}

			processor := NewEventProcessor(tx)
			for _, idx := range perm {
				require.NoError(t, processor.processEvent(events[idx]),
					"event %s must process without error in order %v", defs[idx].label, perm)
			}

			var animal models.ConsolidatedAnimal
			require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", orderInstance, orderAnimal).First(&animal))

			// The final state is ALWAYS the latest-by-timestamp event: the
			// second died state with its Décédé outtake.
			assert.Equal(t, "died", animal.CurrentStatus, "perm %v: final status must be the newest event's", perm)
			assert.Equal(t, "Décédé", animal.OuttakeType.String, "perm %v: outtake must come from the newest state", perm)
			assert.True(t, animal.OuttakeDead.Bool, "perm %v: death must carry the dead flag", perm)
			assert.Equal(t, "C2", animal.Cage.String, "perm %v: snapshot fields must come from the newest state", perm)
			assert.Equal(t, "hash-died-2", animal.StateHash.String, "perm %v: stored hash must be the newest state hash", perm)
			assert.Equal(t, defs[3].at.Unix(), animal.LastStateAt.Time.Unix(),
				"perm %v: last_state_at must hold the newest applied event time", perm)

			// Bookkeeping: every event was processed exactly once.
			for i, ev := range events {
				var reloaded models.EventStream
				require.NoError(t, tx.Find(&reloaded, ev.ID))
				assert.NotNil(t, reloaded.ProcessedAt,
					"perm %v: skipped-or-applied event %s must stay recorded as processed", perm, defs[i].label)
			}
		})
	}
}

func permLabel(perm []int) string {
	s := "perm"
	for _, p := range perm {
		s += "-" + string(rune('0'+p))
	}
	return s
}

// TestEventProcessor_DeleteNotResurrected pins the animal_deleted semantics
// under arbitrary redelivery order:
//   - a delete arriving after old events still deletes;
//   - an OLD event arriving AFTER the delete must not resurrect the row;
//   - a NEWER event after the delete re-establishes the animal and clears
//     the tombstone.
func TestEventProcessor_DeleteNotResurrected(t *testing.T) {
	tx := setupTest(t)
	defs := orderLifecycleEvents(t)
	processor := NewEventProcessor(tx)

	events := make([]*models.EventStream, len(defs))
	for i, def := range defs {
		ev := *def.ev
		ev.ID = uuid.Must(uuid.NewV4())
		ev.CreatedAt = def.at
		require.NoError(t, tx.Create(&ev))
		events[i] = &ev
	}

	// Worst-case order: the delete FIRST (before any state), then the whole
	// stale history replays after it.
	deleteAt := mustTs(t, "2024-09-01T12:00:00Z")
	deleted := &models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: orderInstance, AnimalID: orderAnimal,
		EventType: models.EventTypeAnimalDeleted,
		Payload:   []byte(`{"animal":{"id":2058},"current_status":"deleted","timestamp":"2024-09-01T12:00:00Z"}`),
		CreatedAt: deleteAt,
	}
	require.NoError(t, tx.Create(deleted))

	require.NoError(t, processor.processEvent(deleted), "the delete itself must process")

	rowExists := func() bool {
		exists, err := tx.Where("instance_id = ? AND animal_id = ?", orderInstance, orderAnimal).
			Exists(&models.ConsolidatedAnimal{})
		require.NoError(t, err)
		return exists
	}
	require.False(t, rowExists(), "delete on empty state must not create a row")

	tomb, err := models.FindTombstone(tx, orderInstance, orderAnimal)
	require.NoError(t, err)
	require.NotNil(t, tomb, "a delete must leave a tombstone guarding the animal")
	assert.Equal(t, deleteAt.Unix(), tomb.EventAt.Unix())

	// The stale history replays AFTER the delete (full-redelivery scramble):
	// nothing may resurrect the animal, and no event trace may remain.
	for _, idx := range []int{3, 2, 1, 0} {
		require.NoError(t, processor.processEvent(events[idx]),
			"stale event %s must be absorbed after the delete", defs[idx].label)
	}
	assert.False(t, rowExists(), "an old event after the delete must NOT re-create the row")

	tombCount, err := tx.Where("instance_id = ? AND animal_id = ?", orderInstance, orderAnimal).
		Count(&models.ConsolidatedAnimalTombstone{})
	require.NoError(t, err)
	assert.Equal(t, 1, tombCount, "tombstone must survive stale replays")

	leftEvents, err := tx.Where("instance_id = ? AND animal_id = ?", orderInstance, orderAnimal).
		Count(&models.EventStream{})
	require.NoError(t, err)
	assert.Zero(t, leftEvents, "stale replays of a destroyed animal must leave no event rows")

	// A NEWER event (the animal lives again on the producer — not reachable
	// with the current destroy flow, but the guard must not brick the animal)
	// re-establishes the row and clears the tombstone.
	rebornAt := mustTs(t, "2024-10-01T09:00:00Z")
	reborn := &models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: orderInstance, AnimalID: orderAnimal,
		EventType: models.EventTypeAnimalDiscovered,
		Payload:   []byte(`{"animal":{"id":2058,"species":"Renard"},"current_status":"in_care","timestamp":"2024-10-01T09:00:00Z"}`),
		CreatedAt: rebornAt,
	}
	require.NoError(t, tx.Create(reborn))
	require.NoError(t, processor.processEvent(reborn))
	assert.True(t, rowExists(), "an event newer than the delete re-establishes the animal")

	tombCount, err = tx.Where("instance_id = ? AND animal_id = ?", orderInstance, orderAnimal).
		Count(&models.ConsolidatedAnimalTombstone{})
	require.NoError(t, err)
	assert.Zero(t, tombCount, "re-establishing the animal clears its tombstone")
}

// TestEventProcessor_DeleteAfterOldEventsStillDeletes pins the other half of
// the delete rule: a delete arriving after older events (delete newer than
// everything applied) removes the row and history.
func TestEventProcessor_DeleteAfterOldEventsStillDeletes(t *testing.T) {
	tx := setupTest(t)
	defs := orderLifecycleEvents(t)
	processor := NewEventProcessor(tx)

	for _, def := range defs {
		ev := *def.ev
		ev.ID = uuid.Must(uuid.NewV4())
		ev.CreatedAt = def.at
		require.NoError(t, tx.Create(&ev))
		require.NoError(t, processor.processEvent(&ev))
	}

	deleted := &models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: orderInstance, AnimalID: orderAnimal,
		EventType: models.EventTypeAnimalDeleted,
		Payload:   []byte(`{"animal":{"id":2058},"current_status":"deleted","timestamp":"2024-09-01T12:00:00Z"}`),
		CreatedAt: mustTs(t, "2024-09-01T12:00:00Z"),
	}
	require.NoError(t, tx.Create(deleted))
	require.NoError(t, processor.processEvent(deleted))

	exists, err := tx.Where("instance_id = ? AND animal_id = ?", orderInstance, orderAnimal).
		Exists(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.False(t, exists, "a newer delete after old events must delete the row")

	leftEvents, err := tx.Where("instance_id = ? AND animal_id = ?", orderInstance, orderAnimal).
		Count(&models.EventStream{})
	require.NoError(t, err)
	assert.Zero(t, leftEvents, "the delete removes the animal's whole event history")
}

// TestEventProcessor_StaleDeleteKeepsNewerState pins the stale-delete rule:
// a delete OLDER than the newest applied state is a processed no-op — the
// newer state stands and the event log stays untouched.
func TestEventProcessor_StaleDeleteKeepsNewerState(t *testing.T) {
	tx := setupTest(t)
	defs := orderLifecycleEvents(t)
	processor := NewEventProcessor(tx)

	for _, def := range defs {
		ev := *def.ev
		ev.ID = uuid.Must(uuid.NewV4())
		ev.CreatedAt = def.at
		require.NoError(t, tx.Create(&ev))
		require.NoError(t, processor.processEvent(&ev))
	}

	var before models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", orderInstance, orderAnimal).First(&before))

	// An ANCIENT delete (predating the applied history) arrives late.
	staleDelete := &models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: orderInstance, AnimalID: orderAnimal,
		EventType: models.EventTypeAnimalDeleted,
		Payload:   []byte(`{"animal":{"id":2058},"current_status":"deleted","timestamp":"2024-04-01T00:00:00Z"}`),
		CreatedAt: mustTs(t, "2024-04-01T00:00:00Z"),
	}
	require.NoError(t, tx.Create(staleDelete))
	require.NoError(t, processor.processEvent(staleDelete))

	var after models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", orderInstance, orderAnimal).First(&after))
	assert.Equal(t, before.ID, after.ID, "the newer applied state must stand")
	assert.Equal(t, before.CurrentStatus, after.CurrentStatus)
	assert.Equal(t, before.EventCount, after.EventCount, "a stale delete must not touch the row")

	exists, err := tx.Where("id = ?", staleDelete.ID).Exists(&models.EventStream{})
	require.NoError(t, err)
	assert.True(t, exists, "the stale delete is recorded as processed, not destructive")
}

// TestEventProcessor_MissingTombstoneTableDegradesGracefully proves the
// pre-migration tolerance: on a database WITHOUT the tombstone table (the
// console binary restarted by the watcher before `buffalo db migrate up`
// ran), event processing must keep working — the resurrection guard is
// simply inactive — instead of failing every event of a fresh animal.
func TestEventProcessor_MissingTombstoneTableDegradesGracefully(t *testing.T) {
	db, err := pop.NewConnection(&pop.ConnectionDetails{Dialect: "sqlite", Database: ":memory:"})
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
	require.NoError(t, db.RawQuery(`
		CREATE TABLE consolidated_animals (
			id TEXT PRIMARY KEY,
			instance_id TEXT NOT NULL,
			animal_id INTEGER NOT NULL,
			year INTEGER DEFAULT 0,
			year_number INTEGER DEFAULT 0,
			species TEXT,
			gender TEXT,
			cage TEXT,
			zone TEXT,
			ring TEXT,
			animal_type TEXT,
			animal_age TEXT,
			discovery_location TEXT,
			discovery_date TIMESTAMP,
			discovery_city TEXT,
			discovery_postal_code TEXT,
			discovery_commune TEXT,
			discovery_province TEXT,
			discovery_region TEXT,
			discovery_country TEXT,
			discovery_cantonnement TEXT,
			discovery_direction TEXT,
			entry_cause TEXT,
			entry_cause_detail TEXT,
			entry_cause_nature TEXT,
			species_class TEXT,
			species_agw_group TEXT,
			species_subside_group TEXT,
			species_native_status TEXT,
			species_family TEXT,
			species_order TEXT,
			species_game BOOLEAN,
			species_huntable BOOLEAN,
			entry_cause_id TEXT,
			discoverer_firstname TEXT,
			discoverer_lastname TEXT,
			discoverer_address TEXT,
			discoverer_city TEXT,
			discoverer_postal_code TEXT,
			discoverer_country TEXT,
			discoverer_email TEXT,
			discoverer_phone TEXT,
			discoverer_note TEXT,
			discoverer_donation TEXT,
			current_status TEXT NOT NULL,
			intake_date TIMESTAMP,
			intake_general TEXT,
			intake_wounds TEXT,
			intake_parasites TEXT,
			intake_remarks TEXT,
			outtake_date TIMESTAMP,
			outtake_type TEXT,
			outtake_location TEXT,
			outtake_rating INTEGER,
			outtake_dead BOOLEAN,
			outtake_error BOOLEAN,
			outtake_precise_location TEXT,
			outtake_stay_duration INTEGER,
			outtake_corpse_destination TEXT,
			outtake_corpse_destination_at TIMESTAMP,
			ready_for_release BOOLEAN,
			translations TEXT,
			state_hash TEXT,
			last_state_at TIMESTAMP,
			last_event_at TIMESTAMP NOT NULL,
			event_count INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`).Exec())
	// Deliberately NO consolidated_animal_tombstones table.

	processor := NewEventProcessor(db)

	discovered := &models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-m", AnimalID: 1,
		EventType: models.EventTypeAnimalDiscovered,
		Payload:   []byte(`{"animal":{"species":"Renard"},"current_status":"in_care","timestamp":"2026-01-01T00:00:00Z"}`),
		CreatedAt: mustTs(t, "2026-01-01T00:00:00Z"),
	}
	require.NoError(t, db.Create(discovered))
	require.NoError(t, processor.processEvent(discovered),
		"a fresh animal must process even without the tombstone table")

	exists, err := db.Where("instance_id = ? AND animal_id = ?", "center-m", 1).
		Exists(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.True(t, exists, "the consolidated row must be created (guard degraded, not the pipeline)")

	// A delete must also keep working (marker recording silently skipped).
	deleted := &models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-m", AnimalID: 1,
		EventType: models.EventTypeAnimalDeleted,
		Payload:   []byte(`{"animal":{"id":1},"current_status":"deleted","timestamp":"2026-02-01T00:00:00Z"}`),
		CreatedAt: mustTs(t, "2026-02-01T00:00:00Z"),
	}
	require.NoError(t, db.Create(deleted))
	require.NoError(t, processor.processEvent(deleted), "a delete must process without the tombstone table")

	exists, err = db.Where("instance_id = ? AND animal_id = ?", "center-m", 1).
		Exists(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.False(t, exists, "the delete must still remove the row")
}

// TestEventProcessor_EqualTimestampStillApplies pins the live-event rule:
// an event with the SAME timestamp as the newest applied event applies
// (strictly-older guard only) — live events share the producer's
// second-precision timestamps with adjacent state events.
func TestEventProcessor_EqualTimestampStillApplies(t *testing.T) {
	tx := setupTest(t)
	processor := NewEventProcessor(tx)

	ts := "2026-10-07T23:49:39Z"
	first := &models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: orderInstance, AnimalID: orderAnimal,
		EventType: models.EventTypeAnimalState,
		Payload:   []byte(`{"animal":{"id":2058,"cage":"C1"},"current_status":"in_care","state_hash":"hash-t","timestamp":"` + ts + `"}`),
		CreatedAt: mustTs(t, ts),
	}
	second := &models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: orderInstance, AnimalID: orderAnimal,
		EventType: models.EventTypeAnimalDied,
		Payload:   []byte(`{"animal":{"id":2058},"current_status":"died","outtake":{"type":"Décédé","dead":true},"timestamp":"` + ts + `"}`),
		CreatedAt: mustTs(t, ts),
	}
	require.NoError(t, tx.Create(first))
	require.NoError(t, tx.Create(second))

	require.NoError(t, processor.processEvent(first))
	require.NoError(t, processor.processEvent(second), "an equal timestamp must not be dropped")

	var animal models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", orderInstance, orderAnimal).First(&animal))
	assert.Equal(t, "died", animal.CurrentStatus, "the same-second live event must apply")
}
