package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gobuffalo/nulls"
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

func TestConsolidatedAnimalUpdateFromPayload_NewContractFields(t *testing.T) {
	ca := &ConsolidatedAnimal{ID: uuid.Must(uuid.NewV4()), InstanceID: "test", AnimalID: 7, LastEventAt: time.Now()}
	payload := EventPayload{
		Animal: AnimalPayload{
			Species:             "Hérisson",
			SpeciesClass:        "Mammalia",
			SpeciesAGWGroup:     "agw-1",
			SpeciesSubsideGroup: "sub-1",
			SpeciesNativeStatus: "Indigène",
		},
		Discovery: DiscoveryPayload{
			EntryCause:       "Accident ⇨ Collision",
			EntryCauseDetail: "Collision véhicule",
			EntryCauseNature: "Traumatique",
		},
		Outtake:       OuttakePayload{Type: "Relâché", Rating: 1, Dead: true},
		CurrentStatus: "released",
		Timestamp:     time.Now().Format(time.RFC3339),
	}
	ca.UpdateFromPayload(payload, EventTypeAnimalReleased, time.Now())
	if ca.SpeciesClass.String != "Mammalia" || ca.SpeciesAGWGroup.String != "agw-1" || ca.SpeciesSubsideGroup.String != "sub-1" || ca.SpeciesNativeStatus.String != "Indigène" {
		t.Errorf("taxonomy = %q/%q/%q/%q", ca.SpeciesClass.String, ca.SpeciesAGWGroup.String, ca.SpeciesSubsideGroup.String, ca.SpeciesNativeStatus.String)
	}
	if ca.EntryCauseDetail.String != "Collision véhicule" || ca.EntryCauseNature.String != "Traumatique" {
		t.Errorf("entry cause detail/nature = %q/%q", ca.EntryCauseDetail.String, ca.EntryCauseNature.String)
	}
	if !ca.OuttakeRating.Valid || ca.OuttakeRating.Int != 1 {
		t.Errorf("outtake_rating = %+v, want 1", ca.OuttakeRating)
	}
	if !ca.OuttakeDead.Valid || !ca.OuttakeDead.Bool {
		t.Errorf("outtake_dead = %+v, want true", ca.OuttakeDead)
	}
}

func TestConsolidatedAnimalApplyState_ResetsNewFields(t *testing.T) {
	ca := &ConsolidatedAnimal{
		ID: uuid.Must(uuid.NewV4()), InstanceID: "test", AnimalID: 7,
		SpeciesClass:        nulls.NewString("Mammalia"),
		SpeciesAGWGroup:     nulls.NewString("agw-1"),
		SpeciesSubsideGroup: nulls.NewString("sub-1"),
		SpeciesNativeStatus: nulls.NewString("Indigène"),
		EntryCauseDetail:    nulls.NewString("Collision"),
		EntryCauseNature:    nulls.NewString("Traumatique"),
		OuttakeRating:       nulls.NewInt(1),
		OuttakeDead:         nulls.NewBool(true),
		LastEventAt:         time.Now(),
	}
	// State snapshot omitting the new fields (old producer) must clear them.
	ca.applyState(EventPayload{CurrentStatus: "in_care", Timestamp: time.Now().Format(time.RFC3339)}, time.Now())
	for name, valid := range map[string]bool{
		"species_class":         ca.SpeciesClass.Valid,
		"species_agw_group":     ca.SpeciesAGWGroup.Valid,
		"species_subside_group": ca.SpeciesSubsideGroup.Valid,
		"species_native_status": ca.SpeciesNativeStatus.Valid,
		"entry_cause_detail":    ca.EntryCauseDetail.Valid,
		"entry_cause_nature":    ca.EntryCauseNature.Valid,
		"outtake_rating":        ca.OuttakeRating.Valid,
		"outtake_dead":          ca.OuttakeDead.Valid,
	} {
		if valid {
			t.Errorf("%s still set after state reset", name)
		}
	}
}

func TestConsolidatedAnimalLocalizedField_NewFields(t *testing.T) {
	translations := `{"de":{"species_class":"Säugetiere","entry_cause_detail":"Fahrzeugkollision"}}`
	ca := &ConsolidatedAnimal{
		SpeciesClass:     nulls.NewString("Mammalia"),
		EntryCauseDetail: nulls.NewString("Collision véhicule"),
		OuttakeRating:    nulls.NewInt(2),
		OuttakeDead:      nulls.NewBool(true),
		Translations:     nulls.NewString(translations),
	}
	if got := ca.LocalizedField("de", "species_class"); got != "Säugetiere" {
		t.Errorf("de species_class = %q", got)
	}
	if got := ca.LocalizedField("fr", "species_class"); got != "Mammalia" {
		t.Errorf("fr species_class fallback = %q", got)
	}
	if got := ca.LocalizedField("de", "entry_cause_detail"); got != "Fahrzeugkollision" {
		t.Errorf("de entry_cause_detail = %q", got)
	}
	if got := ca.LocalizedField("de", "outtake_rating"); got != "2" {
		t.Errorf("outtake_rating = %q, want 2", got)
	}
	if got := ca.LocalizedField("de", "outtake_dead"); got != "true" {
		t.Errorf("outtake_dead = %q, want true", got)
	}
}

func TestEventPayload_OmitemptyBackwardsCompat(t *testing.T) {
	// Old producer payload (new fields absent) unmarshals into the new structs.
	oldJSON := []byte(`{"animal":{"id":1,"species":"Hérisson"},"discovery":{"entry_cause":"Accident"},"outtake":{"type":"Relâché"},"timestamp":"2024-01-15T10:30:00Z"}`)
	var payload EventPayload
	if err := json.Unmarshal(oldJSON, &payload); err != nil {
		t.Fatalf("unmarshal old payload: %v", err)
	}
	if payload.Animal.SpeciesClass != "" || payload.Discovery.EntryCauseDetail != "" || payload.Outtake.Rating != 0 || payload.Outtake.Dead {
		t.Errorf("new fields = %+v / %+v / %+v, want zero values", payload.Animal, payload.Discovery, payload.Outtake)
	}
}
