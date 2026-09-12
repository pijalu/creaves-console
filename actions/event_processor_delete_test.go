//go:build sqlite
// +build sqlite

package actions

import (
	"testing"
	"time"

	"creaves-console/models"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deletedEventPayload builds an animal_deleted payload (current_status
// "deleted" like the producer's PublishAnimalDeletedEvent).
func deletedEventPayload() string {
	return `{"animal":{"id":7,"year":2024,"year_number":3,"species":"Renard"},"current_status":"deleted","timestamp":"2024-03-01T10:00:00Z"}`
}

// errorOuttakePayload builds an animal_state payload whose outtake carries
// the producer error flag (destroy flow attaches the error outtake type).
func errorOuttakePayload() string {
	return `{"animal":{"id":7,"year":2024,"year_number":3},"current_status":"in_care","outtake":{"type":"Doublon","rating":-1,"dead":false,"error":true},"timestamp":"2024-03-01T10:00:00Z"}`
}

// TestEventProcessor_AnimalDeletedRemovesConsolidatedAnimal proves the
// console end of the destroy flow (bugs.md "Delete show as deceased in
// console"): an animal_deleted event removes the consolidated row instead of
// marking the animal deceased, and drops the animal's event history so the
// redelivery recovery path cannot resurrect it.
func TestEventProcessor_AnimalDeletedRemovesConsolidatedAnimal(t *testing.T) {
	tx := setupTest(t)
	now := time.Now()

	// Baseline: animal discovered (row + processed event exist).
	discovered := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "center-a",
		AnimalID:   7,
		EventType:  models.EventTypeAnimalDiscovered,
		Payload:    []byte(`{"animal":{"species":"Renard"},"current_status":"in_care","timestamp":"2024-01-01T00:00:00Z"}`),
		CreatedAt:  now.Add(-time.Hour),
	}
	require.NoError(t, tx.Create(discovered))
	processor := NewEventProcessor(tx)
	_, err := processor.ProcessUnprocessedEvents()
	require.NoError(t, err)

	rowExists := func() bool {
		exists, err := tx.Where("instance_id = ? AND animal_id = ?", "center-a", 7).Exists(&models.ConsolidatedAnimal{})
		require.NoError(t, err)
		return exists
	}
	require.True(t, rowExists(), "baseline row must exist")

	// The producer destroys the record: an animal_deleted event arrives.
	deleted := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "center-a",
		AnimalID:   7,
		EventType:  models.EventTypeAnimalDeleted,
		Payload:    []byte(deletedEventPayload()),
		CreatedAt:  now,
	}
	require.NoError(t, tx.Create(deleted))
	count, err := processor.ProcessUnprocessedEvents()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// The consolidated row is GONE, not deceased.
	assert.False(t, rowExists(), "deleted animal must not stay in the consolidated view")

	// The event history of the animal is gone too (no resurrection via
	// redelivery recovery, no checksum divergence).
	eventsLeft, err := tx.Where("instance_id = ? AND animal_id = ?", "center-a", 7).Count(&models.EventStream{})
	require.NoError(t, err)
	assert.Zero(t, eventsLeft, "deleted animal event history must be removed")
}

// TestEventProcessor_ErrorOuttakeStateDeletesRow proves the legacy cleanup
// path: a state snapshot (or any event) whose outtake carries the producer's
// error flag removes the animal — covers animals destroyed BEFORE the
// animal_deleted event type existed (their payloads carry the error outtake).
func TestEventProcessor_ErrorOuttakeStateDeletesRow(t *testing.T) {
	tx := setupTest(t)
	now := time.Now()

	discovered := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "center-a",
		AnimalID:   7,
		EventType:  models.EventTypeAnimalDiscovered,
		Payload:    []byte(`{"animal":{"species":"Renard"},"current_status":"in_care","timestamp":"2024-01-01T00:00:00Z"}`),
		CreatedAt:  now.Add(-time.Hour),
	}
	require.NoError(t, tx.Create(discovered))
	processor := NewEventProcessor(tx)
	_, err := processor.ProcessUnprocessedEvents()
	require.NoError(t, err)

	// Legacy destroy event: animal_died with an error outtake in the payload
	// (exactly what the old producer sent for destroyed records).
	legacy := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "center-a",
		AnimalID:   7,
		EventType:  models.EventTypeAnimalDied,
		Payload:    []byte(errorOuttakePayload()),
		CreatedAt:  now,
	}
	require.NoError(t, tx.Create(legacy))
	_, err = processor.ProcessUnprocessedEvents()
	require.NoError(t, err)

	exists, err := tx.Where("instance_id = ? AND animal_id = ?", "center-a", 7).Exists(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.False(t, exists, "error-outtake events must remove the animal, not mark it deceased")

	// Normal (non-error) events still work after the delete: a fresh state
	// snapshot recreates the animal only if it does NOT carry the error flag.
	fresh := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "center-a",
		AnimalID:   7,
		EventType:  models.EventTypeAnimalState,
		Payload:    []byte(`{"animal":{"species":"Renard"},"current_status":"in_care","state_hash":"h1","timestamp":"2024-03-02T10:00:00Z"}`),
		CreatedAt:  now.Add(time.Hour),
	}
	require.NoError(t, tx.Create(fresh))
	_, err = processor.ProcessUnprocessedEvents()
	require.NoError(t, err)
	exists, err = tx.Where("instance_id = ? AND animal_id = ?", "center-a", 7).Exists(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.True(t, exists, "a normal state snapshot after the delete recreates the animal")
}

// TestEventProcessor_DeletedEventRedeliveryIsIdempotent proves replaying the
// same batch is safe: the delete path does not fail on a missing row.
func TestEventProcessor_DeletedEventRedeliveryIsIdempotent(t *testing.T) {
	tx := setupTest(t)
	processor := NewEventProcessor(tx)

	// No prior rows at all: processing a delete event for an unknown animal
	// must succeed (idempotent no-op) and leave nothing behind.
	event := &models.EventStream{
		ID:         uuid.Must(uuid.NewV4()),
		InstanceID: "center-a",
		AnimalID:   99,
		EventType:  models.EventTypeAnimalDeleted,
		Payload:    []byte(deletedEventPayload()),
		CreatedAt:  time.Now(),
	}
	require.NoError(t, tx.Create(event))

	count, err := processor.ProcessUnprocessedEvents()
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	exists, err := tx.Where("instance_id = ? AND animal_id = ?", "center-a", 99).Exists(&models.ConsolidatedAnimal{})
	require.NoError(t, err)
	assert.False(t, exists)
}
