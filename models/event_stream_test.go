package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventStreamString(t *testing.T) {
	e := EventStream{
		InstanceID: "inst-1",
		AnimalID:   7,
		EventType:  EventTypeAnimalDiscovered,
		SourceDB:   "source_a",
	}

	out := e.String()

	var got EventStream
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("String() did not return valid JSON: %v", err)
	}
	if got.InstanceID != "inst-1" {
		t.Errorf("expected InstanceID %q, got %q", "inst-1", got.InstanceID)
	}
	if got.EventType != EventTypeAnimalDiscovered {
		t.Errorf("expected EventType %q, got %q", EventTypeAnimalDiscovered, got.EventType)
	}
}

func TestEventStreamsString(t *testing.T) {
	es := EventStreams{
		{InstanceID: "a"},
		{InstanceID: "b"},
	}

	out := es.String()

	var got EventStreams
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("String() did not return valid JSON: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(got))
	}
	if got[0].InstanceID != "a" {
		t.Errorf("expected first InstanceID %q, got %q", "a", got[0].InstanceID)
	}
}

func TestEventStreamSetPayload(t *testing.T) {
	e := EventStream{}
	payload := EventPayload{
		Animal:    AnimalPayload{Species: "Fox", Year: 2023},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if err := e.SetPayload(payload); err != nil {
		t.Fatalf("SetPayload returned error: %v", err)
	}
	if len(e.Payload) == 0 {
		t.Fatal("expected Payload to be populated after SetPayload")
	}
	// Payload must be valid JSON.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &raw); err != nil {
		t.Errorf("expected Payload to be valid JSON, got error: %v", err)
	}
}

func TestEventStreamGetPayloadEmpty(t *testing.T) {
	e := EventStream{}

	got, err := e.GetPayload()
	if err != nil {
		t.Fatalf("GetPayload on empty payload returned error: %v", err)
	}
	// An empty payload must yield the zero-value EventPayload, not an error.
	if got.Animal.Species != "" {
		t.Errorf("expected zero-value payload, got Species %q", got.Animal.Species)
	}
}

func TestEventStreamPayloadRoundTrip(t *testing.T) {
	e := EventStream{}
	payload := EventPayload{
		Animal: AnimalPayload{
			Year:       2024,
			YearNumber: 9,
			Species:    "Hedgehog",
			AnimalType: "Mammal",
		},
		Discovery: DiscoveryPayload{
			Location: "Forest edge",
			City:     "Greenville",
			Date:     "2024/05/01 14:30",
		},
		Intake: IntakePayload{
			General: "Dehydrated",
		},
		InitialStatus: "in_care",
		CurrentStatus: "in_care",
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	if err := e.SetPayload(payload); err != nil {
		t.Fatalf("SetPayload returned error: %v", err)
	}

	got, err := e.GetPayload()
	if err != nil {
		t.Fatalf("GetPayload returned error: %v", err)
	}
	if got.Animal.Species != "Hedgehog" {
		t.Errorf("expected Species %q, got %q", "Hedgehog", got.Animal.Species)
	}
	if got.Animal.Year != 2024 {
		t.Errorf("expected Year 2024, got %d", got.Animal.Year)
	}
	if got.Discovery.City != "Greenville" {
		t.Errorf("expected Discovery City %q, got %q", "Greenville", got.Discovery.City)
	}
	if got.Intake.General != "Dehydrated" {
		t.Errorf("expected Intake General %q, got %q", "Dehydrated", got.Intake.General)
	}
	if got.CurrentStatus != "in_care" {
		t.Errorf("expected CurrentStatus %q, got %q", "in_care", got.CurrentStatus)
	}
}
