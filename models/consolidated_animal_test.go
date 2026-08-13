package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestConsolidatedAnimalString(t *testing.T) {
	c := ConsolidatedAnimal{
		InstanceID:    "inst-1",
		AnimalID:      3,
		CurrentStatus: "in_care",
		EventCount:    5,
	}

	out := c.String()

	var got ConsolidatedAnimal
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("String() did not return valid JSON: %v", err)
	}
	if got.InstanceID != "inst-1" {
		t.Errorf("expected InstanceID %q, got %q", "inst-1", got.InstanceID)
	}
	if got.EventCount != 5 {
		t.Errorf("expected EventCount 5, got %d", got.EventCount)
	}
}

func TestConsolidatedAnimalsString(t *testing.T) {
	cs := ConsolidatedAnimals{
		{CurrentStatus: "in_care"},
		{CurrentStatus: "released"},
	}

	out := cs.String()

	var got ConsolidatedAnimals
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("String() did not return valid JSON: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 animals, got %d", len(got))
	}
	if got[1].CurrentStatus != "released" {
		t.Errorf("expected second CurrentStatus %q, got %q", "released", got[1].CurrentStatus)
	}
}

func newConsolidatedAnimal() *ConsolidatedAnimal {
	return &ConsolidatedAnimal{CurrentStatus: "unknown"}
}

func TestConsolidatedAnimalUpdateFromPayloadDiscoveredDefault(t *testing.T) {
	c := newConsolidatedAnimal()
	payload := EventPayload{
		Animal:    AnimalPayload{Species: "Fox", Year: 2024, YearNumber: 1},
		Timestamp: "2024-01-01T00:00:00Z",
	}
	eventTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	c.UpdateFromPayload(payload, EventTypeAnimalDiscovered, eventTime)

	// Empty CurrentStatus on discovery defaults to "in_care".
	if c.CurrentStatus != "in_care" {
		t.Errorf("expected default status %q, got %q", "in_care", c.CurrentStatus)
	}
	if c.Year != 2024 {
		t.Errorf("expected Year 2024, got %d", c.Year)
	}
	if !c.Species.Valid || c.Species.String != "Fox" {
		t.Errorf("expected Species %q, got %+v", "Fox", c.Species)
	}
	if !c.LastEventAt.Equal(eventTime) {
		t.Errorf("expected LastEventAt %v, got %v", eventTime, c.LastEventAt)
	}
	if c.EventCount != 1 {
		t.Errorf("expected EventCount 1, got %d", c.EventCount)
	}
}

func TestConsolidatedAnimalUpdateFromPayloadDiscoveredExplicitStatus(t *testing.T) {
	c := newConsolidatedAnimal()
	payload := EventPayload{CurrentStatus: "quarantine"}

	c.UpdateFromPayload(payload, EventTypeAnimalDiscovered, time.Now())

	if c.CurrentStatus != "quarantine" {
		t.Errorf("expected explicit status %q, got %q", "quarantine", c.CurrentStatus)
	}
}

func TestConsolidatedAnimalUpdateFromPayloadStatusChanged(t *testing.T) {
	c := newConsolidatedAnimal()
	c.CurrentStatus = "in_care"
	payload := EventPayload{CurrentStatus: "in_rehab"}

	c.UpdateFromPayload(payload, EventTypeAnimalStatusChanged, time.Now())

	if c.CurrentStatus != "in_rehab" {
		t.Errorf("expected status %q, got %q", "in_rehab", c.CurrentStatus)
	}
}

func TestConsolidatedAnimalUpdateFromPayloadStatusChangedIgnoredWhenEmpty(t *testing.T) {
	c := newConsolidatedAnimal()
	c.CurrentStatus = "in_care"
	payload := EventPayload{} // empty CurrentStatus must not overwrite existing status

	c.UpdateFromPayload(payload, EventTypeAnimalStatusChanged, time.Now())

	if c.CurrentStatus != "in_care" {
		t.Errorf("expected status unchanged %q, got %q", "in_care", c.CurrentStatus)
	}
}

func TestConsolidatedAnimalUpdateFromPayloadReleased(t *testing.T) {
	c := newConsolidatedAnimal()
	c.CurrentStatus = "in_care"
	payload := EventPayload{
		Outtake: OuttakePayload{Type: "release", Location: "Woods", Date: "2024/06/15 09:00"},
	}

	c.UpdateFromPayload(payload, EventTypeAnimalReleased, time.Now())

	if c.CurrentStatus != "released" {
		t.Errorf("expected status %q, got %q", "released", c.CurrentStatus)
	}
	if !c.OuttakeType.Valid || c.OuttakeType.String != "release" {
		t.Errorf("expected OuttakeType %q, got %+v", "release", c.OuttakeType)
	}
	if !c.OuttakeDate.Valid {
		t.Error("expected OuttakeDate to be parsed and set")
	}
}

func TestConsolidatedAnimalUpdateFromPayloadDied(t *testing.T) {
	c := newConsolidatedAnimal()
	c.CurrentStatus = "in_care"

	c.UpdateFromPayload(EventPayload{}, EventTypeAnimalDied, time.Now())

	if c.CurrentStatus != "died" {
		t.Errorf("expected status %q, got %q", "died", c.CurrentStatus)
	}
}

func TestConsolidatedAnimalUpdateFromPayloadDiscoveryDateParsing(t *testing.T) {
	c := newConsolidatedAnimal()
	payload := EventPayload{
		Discovery: DiscoveryPayload{
			Location: "Meadow",
			City:     "Hilltown",
			Date:     "2024/03/10 08:15",
		},
	}

	c.UpdateFromPayload(payload, EventTypeAnimalDiscovered, time.Now())

	if !c.DiscoveryLocation.Valid || c.DiscoveryLocation.String != "Meadow" {
		t.Errorf("expected DiscoveryLocation %q, got %+v", "Meadow", c.DiscoveryLocation)
	}
	if !c.DiscoveryCity.Valid || c.DiscoveryCity.String != "Hilltown" {
		t.Errorf("expected DiscoveryCity %q, got %+v", "Hilltown", c.DiscoveryCity)
	}
	if !c.DiscoveryDate.Valid {
		t.Error("expected DiscoveryDate to be parsed and set")
	} else {
		// The implementation parses via time.Parse(DateTimeFormat, ...); mirror it.
		want, _ := time.Parse(DateTimeFormat, "2024/03/10 08:15")
		if !c.DiscoveryDate.Time.Equal(want) {
			t.Errorf("DiscoveryDate mismatch: want %v, got %v", want, c.DiscoveryDate.Time)
		}
	}
}

func TestConsolidatedAnimalUpdateFromPayloadInvalidDateIgnored(t *testing.T) {
	c := newConsolidatedAnimal()
	payload := EventPayload{
		Discovery: DiscoveryPayload{Date: "not-a-date"},
		Intake:    IntakePayload{Date: "also-not-a-date"},
	}

	c.UpdateFromPayload(payload, EventTypeAnimalDiscovered, time.Now())

	// Invalid dates must not set the corresponding nulls.Time fields.
	if c.DiscoveryDate.Valid {
		t.Error("expected DiscoveryDate to remain invalid for a bad date")
	}
	if c.IntakeDate.Valid {
		t.Error("expected IntakeDate to remain invalid for a bad date")
	}
}

func TestConsolidatedAnimalUpdateFromPayloadEventCountIncrements(t *testing.T) {
	c := newConsolidatedAnimal()

	c.UpdateFromPayload(EventPayload{}, EventTypeAnimalDiscovered, time.Now())
	c.UpdateFromPayload(EventPayload{CurrentStatus: "in_rehab"}, EventTypeAnimalStatusChanged, time.Now())
	c.UpdateFromPayload(EventPayload{}, EventTypeAnimalReleased, time.Now())

	if c.EventCount != 3 {
		t.Errorf("expected EventCount 3 after three updates, got %d", c.EventCount)
	}
}

func TestConsolidatedAnimalApplyEvent(t *testing.T) {
	payload := EventPayload{
		Animal:        AnimalPayload{Species: "Badger", Year: 2024},
		CurrentStatus: "in_care",
		Timestamp:     "2024-01-01T00:00:00Z",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	created := time.Date(2024, 2, 2, 12, 0, 0, 0, time.UTC)
	event := EventStream{
		EventType:  EventTypeAnimalDiscovered,
		Payload:    raw,
		CreatedAt:  created,
	}

	c := newConsolidatedAnimal()
	if err := c.ApplyEvent(event); err != nil {
		t.Fatalf("ApplyEvent returned error: %v", err)
	}

	if c.CurrentStatus != "in_care" {
		t.Errorf("expected CurrentStatus %q, got %q", "in_care", c.CurrentStatus)
	}
	if !c.Species.Valid || c.Species.String != "Badger" {
		t.Errorf("expected Species %q, got %+v", "Badger", c.Species)
	}
	if !c.LastEventAt.Equal(created) {
		t.Errorf("expected LastEventAt %v, got %v", created, c.LastEventAt)
	}
	if c.EventCount != 1 {
		t.Errorf("expected EventCount 1, got %d", c.EventCount)
	}
}
