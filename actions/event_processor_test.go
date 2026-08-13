package actions

import (
	"creaves-console/models"
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
			created_at TIMESTAMP NOT NULL
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
	tables := []string{"event_streams", "consolidated_animals", "webhook_api_keys", "import_runs"}
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
