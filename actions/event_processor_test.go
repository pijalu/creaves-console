//go:build sqlite
// +build sqlite

package actions

import (
	"creaves-console/models"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDB holds the test database connection
var testDB *pop.Connection

func TestCreavesInstanceRegistryModel(t *testing.T) {
	now := time.Now().UTC()
	instance := &models.CreavesInstance{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-north", Name: "North", Description: "Wildlife center",
		FirstSeenAt: now, LastSeenAt: now,
	}
	if _, err := testDB.Eager().ValidateAndCreate(instance); err != nil {
		t.Fatalf("create instance: %v", err)
	}
	var found models.CreavesInstance
	if err := testDB.Where("instance_id = ?", instance.InstanceID).First(&found); err != nil {
		t.Fatalf("find instance: %v", err)
	}
	if found.Name != "North" || found.InstanceID != instance.InstanceID {
		t.Fatalf("unexpected instance: %+v", found)
	}
}

func TestMain(m *testing.M) {
	// Setup test database with SQLite (file-based for migration support)
	var err error
	testDB, err = pop.NewConnection(
		&pop.ConnectionDetails{
			Dialect:  "sqlite",
			Database: "./test.db",
		})
	if err != nil {
		fmt.Printf("Failed to create test database: %v\n", err)
		os.Exit(1)
	}

	if err := testDB.Open(); err != nil {
		fmt.Printf("Failed to open test database: %v\n", err)
		os.Exit(1)
	}

	// Create tables manually for SQLite
	createTables()

	// Run tests
	code := m.Run()

	// Teardown - close and remove database file
	testDB.Close()
	os.Remove("./test.db")

	os.Exit(code)
}

func createTables() {
	// Create instance registry table
	testDB.RawQuery(`
		CREATE TABLE IF NOT EXISTS creaves_instances (
			id TEXT PRIMARY KEY,
			instance_id TEXT NOT NULL UNIQUE,
			name TEXT,
			description TEXT,
			first_seen_at TIMESTAMP NOT NULL,
			last_seen_at TIMESTAMP NOT NULL,
			last_event_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`).Exec()

	// Create event_streams table
	testDB.RawQuery(`
		CREATE TABLE IF NOT EXISTS event_streams (
			id TEXT PRIMARY KEY,
			instance_id TEXT NOT NULL,
			animal_id INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			payload TEXT,
			source_db TEXT NOT NULL DEFAULT '',
			imported_at TIMESTAMP NOT NULL,
			processed_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`).Exec()

	// Create consolidated_animals table with all fields
	testDB.RawQuery(`
		CREATE TABLE IF NOT EXISTS consolidated_animals (
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
			entry_cause TEXT,
			current_status TEXT NOT NULL,
			intake_date TIMESTAMP,
			intake_general TEXT,
			intake_wounds TEXT,
			intake_parasites TEXT,
			intake_remarks TEXT,
			outtake_date TIMESTAMP,
			outtake_type TEXT,
			outtake_location TEXT,
			translations TEXT,
			state_hash TEXT,
			last_state_at TIMESTAMP,
			last_event_at TIMESTAMP NOT NULL,
			event_count INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`).Exec()

	// Create users table
	testDB.RawQuery(`
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			login TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			email TEXT,
			admin BOOLEAN DEFAULT 0,
			active BOOLEAN DEFAULT 1,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`).Exec()

	// Create webhook_api_keys table
	testDB.RawQuery(`
		CREATE TABLE IF NOT EXISTS webhook_api_keys (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL,
			key_prefix TEXT NOT NULL,
			instance_id TEXT,
			active BOOLEAN DEFAULT 1,
			last_used_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`).Exec()

	// Create import_runs table
	testDB.RawQuery(`
		CREATE TABLE IF NOT EXISTS import_runs (
			id TEXT PRIMARY KEY,
			started_at TIMESTAMP NOT NULL,
			completed_at TIMESTAMP,
			source_count INTEGER DEFAULT 0,
			events_imported INTEGER DEFAULT 0,
			events_processed INTEGER DEFAULT 0,
			status TEXT DEFAULT 'running',
			error_message TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`).Exec()

	// Create indexes
	testDB.RawQuery(`CREATE INDEX IF NOT EXISTS idx_event_streams_instance ON event_streams(instance_id, animal_id, created_at)`).Exec()
	testDB.RawQuery(`CREATE INDEX IF NOT EXISTS idx_event_streams_processed ON event_streams(processed_at)`).Exec()
	testDB.RawQuery(`CREATE INDEX IF NOT EXISTS idx_consolidated_instance ON consolidated_animals(instance_id, animal_id)`).Exec()
}

func setupTest(t *testing.T) *pop.Connection {
	// Clean tables before each test
	tables := []string{"event_streams", "consolidated_animals", "webhook_api_keys", "import_runs", "creaves_instances"}
	for _, table := range tables {
		err := testDB.RawQuery("DELETE FROM " + table).Exec()
		require.NoError(t, err)
	}

	return testDB
}

func TestEventProcessor_ProcessUnprocessedEvents(t *testing.T) {
	tx := setupTest(t)

	// Create test events with nested payload structure
	event1 := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "test-instance-1",
		AnimalID:   1,
		EventType:  models.EventTypeAnimalDiscovered,
		Payload:    []byte(`{"animal":{"species":"Fox","year":2024,"year_number":1},"initial_status":"in_care","timestamp":"2024-01-01T00:00:00Z"}`),
		CreatedAt:  time.Now(),
	}
	err := tx.Create(event1)
	require.NoError(t, err)

	event2 := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "test-instance-1",
		AnimalID:   1,
		EventType:  models.EventTypeAnimalReleased,
		Payload:    []byte(`{"outtake":{"type":"Released"},"timestamp":"2024-02-01T00:00:00Z"}`),
		CreatedAt:  time.Now().Add(24 * time.Hour),
	}
	err = tx.Create(event2)
	require.NoError(t, err)

	// Process events
	processor := NewEventProcessor(tx)
	count, err := processor.ProcessUnprocessedEvents()
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify consolidated animal
	var consolidated models.ConsolidatedAnimal
	err = tx.Where("instance_id = ? AND animal_id = ?", "test-instance-1", 1).First(&consolidated)
	require.NoError(t, err)
	assert.Equal(t, "released", consolidated.CurrentStatus)
	assert.Equal(t, 2, consolidated.EventCount)
	assert.Equal(t, "Fox", consolidated.Species.String)
}

func TestEventProcessor_AnimalStateSameHashIsIdempotent(t *testing.T) {
	tx := setupTest(t)

	first := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "test-instance-1",
		AnimalID:   1,
		EventType:  models.EventTypeAnimalState,
		Payload:    []byte(`{"animal":{"species":"Fox","cage":"A1"},"current_status":"in_care","state_hash":"same-state"}`),
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	second := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "test-instance-1",
		AnimalID:   1,
		EventType:  models.EventTypeAnimalState,
		Payload:    []byte(`{"animal":{"species":"Badger","cage":"B2"},"current_status":"died","state_hash":"same-state"}`),
		CreatedAt:  time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}
	require.NoError(t, tx.Create(first))
	require.NoError(t, tx.Create(second))

	count, err := NewEventProcessor(tx).ProcessUnprocessedEvents()
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	var animal models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", "test-instance-1", 1).First(&animal))
	assert.Equal(t, "Fox", animal.Species.String)
	assert.Equal(t, "A1", animal.Cage.String)
	assert.Equal(t, "in_care", animal.CurrentStatus)
	assert.Equal(t, 0, animal.EventCount)
}

func TestEventProcessor_IdempotentProcessing(t *testing.T) {
	tx := setupTest(t)

	// Create test event with nested payload
	event := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "test-instance-1",
		AnimalID:   1,
		EventType:  models.EventTypeAnimalDiscovered,
		Payload:    []byte(`{"animal":{"species":"Fox","year":2024},"initial_status":"in_care","timestamp":"2024-01-01T00:00:00Z"}`),
		CreatedAt:  time.Now(),
	}
	err := tx.Create(event)
	require.NoError(t, err)

	// Process first time
	processor := NewEventProcessor(tx)
	count, err := processor.ProcessUnprocessedEvents()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Get first result
	var firstResult models.ConsolidatedAnimal
	err = tx.Where("instance_id = ? AND animal_id = ?", "test-instance-1", 1).First(&firstResult)
	require.NoError(t, err)

	// Reset processed_at to simulate reprocessing
	err = tx.RawQuery("UPDATE event_streams SET processed_at = NULL").Exec()
	require.NoError(t, err)

	// Process second time
	count, err = processor.ProcessUnprocessedEvents()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Get second result
	var secondResult models.ConsolidatedAnimal
	err = tx.Where("instance_id = ? AND animal_id = ?", "test-instance-1", 1).First(&secondResult)
	require.NoError(t, err)

	// Should be same animal with updated event count
	assert.Equal(t, firstResult.ID, secondResult.ID)
	assert.Equal(t, firstResult.CurrentStatus, secondResult.CurrentStatus)
	assert.Equal(t, firstResult.Species.String, secondResult.Species.String)
}

func TestConsolidationRunner_Run(t *testing.T) {
	tx := setupTest(t)

	// Create test events directly (simulating webhook receipt) with nested payload
	event := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "test-instance-1",
		AnimalID:   1,
		EventType:  models.EventTypeAnimalDiscovered,
		Payload:    []byte(`{"animal":{"species":"Fox","year":2024},"initial_status":"in_care","timestamp":"2024-01-01T00:00:00Z"}`),
		CreatedAt:  time.Now(),
	}
	err := tx.Create(event)
	require.NoError(t, err)

	// Run consolidation
	runner := NewConsolidationRunner(tx)
	result, err := runner.Run()
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, result.EventsProcessed)

	// Verify import run record
	var importRun models.ImportRun
	err = tx.Where("id = ?", result.ImportRunID).First(&importRun)
	require.NoError(t, err)
	assert.Equal(t, "completed", importRun.Status)
	assert.Equal(t, 1, importRun.EventsProcessed)
}

func TestEventProcessor_MultipleInstances(t *testing.T) {
	tx := setupTest(t)

	// Create events from different instances with nested payload
	events := []*models.EventStream{
		{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: "instance-a",
			AnimalID:   1,
			EventType:  models.EventTypeAnimalDiscovered,
			Payload:    []byte(`{"animal":{"species":"Fox"},"initial_status":"in_care"}`),
			CreatedAt:  time.Now(),
		},
		{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: "instance-b",
			AnimalID:   1,
			EventType:  models.EventTypeAnimalDiscovered,
			Payload:    []byte(`{"animal":{"species":"Owl"},"initial_status":"in_care"}`),
			CreatedAt:  time.Now(),
		},
		{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: "instance-a",
			AnimalID:   2,
			EventType:  models.EventTypeAnimalDiscovered,
			Payload:    []byte(`{"animal":{"species":"Hedgehog"},"initial_status":"in_care"}`),
			CreatedAt:  time.Now(),
		},
	}

	for _, event := range events {
		err := tx.Create(event)
		require.NoError(t, err)
	}

	// Process events
	processor := NewEventProcessor(tx)
	count, err := processor.ProcessUnprocessedEvents()
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Verify animals are separate
	var animals models.ConsolidatedAnimals
	err = tx.All(&animals)
	require.NoError(t, err)
	assert.Equal(t, 3, len(animals))

	// Verify instance separation
	var instanceAAnimals models.ConsolidatedAnimals
	err = tx.Where("instance_id = ?", "instance-a").All(&instanceAAnimals)
	require.NoError(t, err)
	assert.Equal(t, 2, len(instanceAAnimals))
}

func TestEventProcessor_StatusTransitions(t *testing.T) {
	tx := setupTest(t)

	// Create a sequence of events for one animal with nested payload
	events := []*models.EventStream{
		{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: "test-instance",
			AnimalID:   1,
			EventType:  models.EventTypeAnimalDiscovered,
			Payload:    []byte(`{"initial_status":"in_care"}`),
			CreatedAt:  time.Now(),
		},
		{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: "test-instance",
			AnimalID:   1,
			EventType:  models.EventTypeAnimalStatusChanged,
			Payload:    []byte(`{"current_status":"under_treatment"}`),
			CreatedAt:  time.Now().Add(time.Hour),
		},
		{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: "test-instance",
			AnimalID:   1,
			EventType:  models.EventTypeAnimalStatusChanged,
			Payload:    []byte(`{"current_status":"in_care"}`),
			CreatedAt:  time.Now().Add(2 * time.Hour),
		},
		{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: "test-instance",
			AnimalID:   1,
			EventType:  models.EventTypeAnimalReleased,
			Payload:    []byte(`{"outtake":{"type":"Released to Wild"}}`),
			CreatedAt:  time.Now().Add(3 * time.Hour),
		},
	}

	for _, event := range events {
		err := tx.Create(event)
		require.NoError(t, err)
	}

	// Process events
	processor := NewEventProcessor(tx)
	count, err := processor.ProcessUnprocessedEvents()
	require.NoError(t, err)
	assert.Equal(t, 4, count)

	// Verify final status
	var animal models.ConsolidatedAnimal
	err = tx.Where("instance_id = ? AND animal_id = ?", "test-instance", 1).First(&animal)
	require.NoError(t, err)
	assert.Equal(t, "released", animal.CurrentStatus)
	assert.Equal(t, 4, animal.EventCount)
	assert.True(t, animal.OuttakeType.Valid)
	assert.Equal(t, "Released to Wild", animal.OuttakeType.String)
}

func TestEventProcessor_GetConsolidatedStats(t *testing.T) {
	tx := setupTest(t)

	// Create test data with nested payload
	events := []*models.EventStream{
		{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: "instance-a",
			AnimalID:   1,
			EventType:  models.EventTypeAnimalDiscovered,
			Payload:    []byte(`{"initial_status":"in_care"}`),
			CreatedAt:  time.Now(),
		},
		{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: "instance-a",
			AnimalID:   2,
			EventType:  models.EventTypeAnimalDiscovered,
			Payload:    []byte(`{"initial_status":"in_care"}`),
			CreatedAt:  time.Now(),
		},
		{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: "instance-b",
			AnimalID:   1,
			EventType:  models.EventTypeAnimalDied,
			Payload:    []byte(`{}`),
			CreatedAt:  time.Now(),
		},
	}

	for _, event := range events {
		err := tx.Create(event)
		require.NoError(t, err)
	}

	// Process events
	processor := NewEventProcessor(tx)
	_, err := processor.ProcessUnprocessedEvents()
	require.NoError(t, err)

	// Get stats
	stats, err := processor.GetConsolidatedStats()
	require.NoError(t, err)
	assert.Equal(t, 3, stats["total_animals"])

	if byStatus, ok := stats["by_status"].(map[string]int); ok {
		assert.Equal(t, 2, byStatus["in_care"])
		assert.Equal(t, 1, byStatus["died"])
	}

	if byInstance, ok := stats["by_instance"].(map[string]int); ok {
		assert.Equal(t, 2, byInstance["instance-a"])
		assert.Equal(t, 1, byInstance["instance-b"])
	}
}

func TestConsolidationRunner_GetRunHistory(t *testing.T) {
	tx := setupTest(t)

	// Create some import runs
	for i := 0; i < 5; i++ {
		run := &models.ImportRun{
			ID:        uuid.Must(uuid.NewV4()),
			Status:    "completed",
			StartedAt: time.Now().Add(time.Duration(i) * time.Hour),
		}
		err := tx.Create(run)
		require.NoError(t, err)
	}

	runner := NewConsolidationRunner(tx)
	history, err := runner.GetRunHistory(3)
	require.NoError(t, err)
	assert.Equal(t, 3, len(history))
}

// ---------------------------------------------------------------------------
// EventProcessor: ProcessAllEvents (rebuild) and ProcessEventsBatch
// ---------------------------------------------------------------------------

func TestEventProcessor_ProcessAllEvents(t *testing.T) {
	tx := setupTest(t)

	// Seed a processed event + its consolidated animal.
	ev := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "inst-1",
		AnimalID:   1,
		EventType:  models.EventTypeAnimalDiscovered,
		Payload:    []byte(`{"animal":{"species":"Fox"},"initial_status":"in_care"}`),
		CreatedAt:  time.Now(),
	}
	require.NoError(t, tx.Create(ev))
	processor := NewEventProcessor(tx)
	_, err := processor.ProcessUnprocessedEvents()
	require.NoError(t, err)

	// ProcessAllEvents resets consolidated_animals and reprocesses everything.
	count, err := processor.ProcessAllEvents()
	require.NoError(t, err)
	assert.Equal(t, 1, count, "all events should be reprocessed")

	// Consolidated animal rebuilt.
	var animal models.ConsolidatedAnimal
	require.NoError(t, tx.Where("instance_id = ? AND animal_id = ?", "inst-1", 1).First(&animal))
	assert.Equal(t, "in_care", animal.CurrentStatus)
}

func TestEventProcessor_ProcessEventsBatch(t *testing.T) {
	tx := setupTest(t)

	// Seed 3 unprocessed events.
	for i := 0; i < 3; i++ {
		require.NoError(t, tx.Create(&models.EventStream{
			ID:         uuid.Must(uuid.NewV4()),
			InstanceID: "inst-1",
			AnimalID:   i + 1,
			EventType:  models.EventTypeAnimalDiscovered,
			Payload:    []byte(`{"initial_status":"in_care"}`),
			CreatedAt:  time.Now(),
		}))
	}

	processor := NewEventProcessor(tx)

	// Process a batch of 2.
	count, done, err := processor.ProcessEventsBatch(2)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.False(t, done, "should not be done — 1 event remains")

	// Process the remainder.
	count, done, err = processor.ProcessEventsBatch(10)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.True(t, done, "should be done now")

	// Empty batch after all processed.
	count, done, err = processor.ProcessEventsBatch(10)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.True(t, done)
}

// ---------------------------------------------------------------------------
// ConsolidationRunner: Run error path, RunDryRun, GetLastRun
// ---------------------------------------------------------------------------

func TestConsolidationRunner_RunDryRun(t *testing.T) {
	tx := setupTest(t)
	runner := NewConsolidationRunner(tx)
	result, err := runner.RunDryRun()
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not yet implemented")
}

func TestConsolidationRunner_GetLastRun(t *testing.T) {
	tx := setupTest(t)

	// No runs → error (no rows).
	runner := NewConsolidationRunner(tx)
	_, err := runner.GetLastRun()
	assert.Error(t, err)

	// Seed two runs at different times.
	first := &models.ImportRun{ID: uuid.Must(uuid.NewV4()), Status: "completed", StartedAt: time.Now().Add(-2 * time.Hour)}
	require.NoError(t, tx.Create(first))
	second := &models.ImportRun{ID: uuid.Must(uuid.NewV4()), Status: "completed", StartedAt: time.Now()}
	require.NoError(t, tx.Create(second))

	last, err := runner.GetLastRun()
	require.NoError(t, err)
	assert.Equal(t, second.ID, last.ID, "should return the most recent run")
}

func TestConsolidationRunner_RunProcessesEvents(t *testing.T) {
	tx := setupTest(t)

	require.NoError(t, tx.Create(&models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "inst-1",
		AnimalID:   1,
		EventType:  models.EventTypeAnimalDiscovered,
		Payload:    []byte(`{"animal":{"species":"Fox"},"initial_status":"in_care"}`),
		CreatedAt:  time.Now(),
	}))

	runner := NewConsolidationRunner(tx)
	result, err := runner.Run()
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, result.EventsProcessed)
	assert.Empty(t, result.Errors)

	// Import run record reflects completion.
	var run models.ImportRun
	require.NoError(t, tx.Find(&run, result.ImportRunID))
	assert.Equal(t, "completed", run.Status)
}

// TestEventProcessor_ClosedDBErrorPaths covers the error-return branches of the
// processor by giving it a closed DB connection. Every query will then fail,
// exercising the defensive error paths without affecting the shared testDB.
func TestEventProcessor_ClosedDBErrorPaths(t *testing.T) {
	// An in-memory SQLite DB with NO tables: every query fails with
	// "no such table", exercising the defensive error returns without a
	// panic (a fully closed connection panics inside pop's SQLite dialect).
	bad, err := pop.NewConnection(&pop.ConnectionDetails{Dialect: "sqlite", Database: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, bad.Open())
	defer bad.Close()
	// Deliberately do NOT create any tables.

	proc := NewEventProcessor(bad)

	// ProcessUnprocessedEvents → query error.
	_, err = proc.ProcessUnprocessedEvents()
	assert.Error(t, err)

	// ProcessAllEvents → raw query error on DELETE.
	_, err = proc.ProcessAllEvents()
	assert.Error(t, err)

	// ProcessEventsBatch → query error.
	_, _, err = proc.ProcessEventsBatch(10)
	assert.Error(t, err)

	// processEvent via findOrCreateConsolidatedAnimal → query error.
	err = proc.processEvent(&models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "inst-1",
		AnimalID:   1,
		EventType:  models.EventTypeAnimalDiscovered,
		Payload:    []byte(`{"initial_status":"in_care"}`),
	})
	assert.Error(t, err)

	// GetConsolidatedStats → query error.
	_, err = proc.GetConsolidatedStats()
	assert.Error(t, err)
}

// TestEventProcessor_ProcessEventInvalidPayload covers the ApplyEvent error
// branch: an event with malformed JSON payload causes processEvent to return
// an error (and is skipped by ProcessUnprocessedEvents).
func TestEventProcessor_ProcessEventInvalidPayload(t *testing.T) {
	tx := setupTest(t)

	// An event whose payload is not valid JSON.
	require.NoError(t, tx.Create(&models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "inst-1",
		AnimalID:   1,
		EventType:  models.EventTypeAnimalDiscovered,
		Payload:    json.RawMessage(`{not valid json`),
		CreatedAt:  time.Now(),
	}))

	proc := NewEventProcessor(tx)

	// Direct call → error.
	err := proc.processEvent(&models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "inst-1",
		AnimalID:   2,
		EventType:  models.EventTypeAnimalDiscovered,
		Payload:    json.RawMessage(`{not valid json`),
		CreatedAt:  time.Now(),
	})
	assert.Error(t, err)

	// Via ProcessUnprocessedEvents → returns count 0 + error.
	count, err := proc.ProcessUnprocessedEvents()
	assert.Error(t, err)
	assert.Equal(t, 0, count)
}

// TestConsolidationRunner_RunCreateFails covers the error path where the import
// run record cannot be created (no tables on the connection).
func TestConsolidationRunner_RunCreateFails(t *testing.T) {
	bad, err := pop.NewConnection(&pop.ConnectionDetails{Dialect: "sqlite", Database: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, bad.Open())
	defer bad.Close()

	runner := NewConsolidationRunner(bad)
	result, err := runner.Run()
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestConsolidationRunner_GetLastRunAndHistoryErrors covers the error paths of
// the history queries on a table-less DB.
func TestConsolidationRunner_GetLastRunAndHistoryErrors(t *testing.T) {
	bad, err := pop.NewConnection(&pop.ConnectionDetails{Dialect: "sqlite", Database: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, bad.Open())
	defer bad.Close()

	runner := NewConsolidationRunner(bad)
	_, err = runner.GetLastRun()
	assert.Error(t, err)

	_, err = runner.GetRunHistory(3)
	assert.Error(t, err)
}
