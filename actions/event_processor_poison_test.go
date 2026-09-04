//go:build sqlite
// +build sqlite

package actions

import (
	"testing"
	"time"

	"creaves-console/models"

	"github.com/gobuffalo/nulls"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A single unprocessable ("poison") event must not block the replay of newer
// events. This is the root cause of the historic "no animals of 2026 after
// full extract" incident: ProcessUnprocessedEvents walked events in
// created_at order and returned on the first error, so one malformed event
// orphaned every event after it — i.e. all newest (current-year) animals.
func TestProcessUnprocessedEvents_PoisonEventDoesNotBlockNewerEvents(t *testing.T) {
	require.NoError(t, testDB.RawQuery("DELETE FROM consolidated_animals").Exec())
	require.NoError(t, testDB.RawQuery("DELETE FROM event_streams").Exec())

	now := time.Now().UTC()

	// Poison: malformed payload JSON, oldest event.
	poison := &models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: 1,
		EventType: models.EventTypeAnimalDiscovered, Payload: []byte(`{"animal":`),
		ImportedAt: now.Add(-2 * time.Hour), CreatedAt: now.Add(-2 * time.Hour),
	}
	require.NoError(t, testDB.Create(poison))

	// Healthy: a 2026 animal discovered after the poison event.
	good := &models.EventStream{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "center-a", AnimalID: 2,
		EventType: models.EventTypeAnimalDiscovered,
		Payload: []byte(`{"animal":{"id":2,"year":2026,"year_number":7,"species":"Hérisson"},"current_status":"in_care"}`),
		ImportedAt: now.Add(-1 * time.Hour), CreatedAt: now.Add(-1 * time.Hour),
	}
	require.NoError(t, testDB.Create(good))

	processed, err := NewEventProcessor(testDB).ProcessUnprocessedEvents()

	// The healthy event must have been applied despite the poison event.
	require.NoError(t, testDB.RawQuery(
		"SELECT COUNT(*) FROM consolidated_animals WHERE instance_id = 'center-a' AND animal_id = 2",
	).First(&processed))
	assert.Equal(t, 1, processed, "the newer healthy event must be processed")

	// The poison event stays unprocessed and the call reports it.
	assert.Equal(t, 1, countUnprocessed(t), "only the poison event may remain unprocessed")
	require.Error(t, err, "the poison event must be reported")
	assert.Contains(t, err.Error(), poison.ID.String(), "the error must identify the poison event")

	// Row content check: the 2026 animal is fully consolidated.
	animal := &models.ConsolidatedAnimal{}
	require.NoError(t, testDB.Where("instance_id = ? AND animal_id = ?", "center-a", 2).First(animal))
	assert.Equal(t, 2026, animal.Year)
	assert.Equal(t, "in_care", animal.CurrentStatus)
	assert.True(t, nulls.NewString("Hérisson").String == animal.Species.String)
}

func countUnprocessed(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.RawQuery("SELECT COUNT(*) FROM event_streams WHERE processed_at IS NULL").First(&n))
	return n
}
