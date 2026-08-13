package models

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
)

func TestConsolidatedAnimalUpdateFromPayload(t *testing.T) {
	ca := &ConsolidatedAnimal{
		ID:            uuid.Must(uuid.NewV4()),
		InstanceID:    "test-instance",
		AnimalID:      1,
		CurrentStatus: "unknown",
		LastEventAt:   time.Now(),
		EventCount:    0,
	}

	payload := EventPayload{
		Animal: AnimalPayload{
			Year:       2024,
			YearNumber: 42,
			Species:    "Test Species",
			AnimalType: "Bird",
			AnimalAge:  "Adult",
		},
		Discovery: DiscoveryPayload{
			Location: "Test Location",
		},
		InitialStatus: "in_care",
		CurrentStatus: "in_care",
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	ca.UpdateFromPayload(payload, EventTypeAnimalDiscovered, time.Now())

	if ca.CurrentStatus != "in_care" {
		t.Errorf("Expected status 'in_care', got '%s'", ca.CurrentStatus)
	}

	if ca.EventCount != 1 {
		t.Errorf("Expected event count 1, got %d", ca.EventCount)
	}

	if ca.Species.String != "Test Species" {
		t.Errorf("Expected species 'Test Species', got '%s'", ca.Species.String)
	}
}

func TestEventPayload(t *testing.T) {
	e := EventStream{}
	payload := EventPayload{
		Animal: AnimalPayload{
			Species: "Fox",
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	err := e.SetPayload(payload)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	retrieved, err := e.GetPayload()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if retrieved.Animal.Species != "Fox" {
		t.Errorf("Expected species 'Fox', got '%s'", retrieved.Animal.Species)
	}
}
