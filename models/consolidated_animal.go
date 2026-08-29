package models

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
	"github.com/gobuffalo/validate/v3"
	"github.com/gofrs/uuid"
)

// DateTimeFormat is the format used for dates in event payloads
const DateTimeFormat = "2006/01/02 15:04"

// ConsolidatedAnimal represents the unified view across all instances
type ConsolidatedAnimal struct {
	ID                  uuid.UUID    `json:"id" db:"id"`
	InstanceID          string       `json:"instance_id" db:"instance_id"`
	AnimalID            int          `json:"animal_id" db:"animal_id"`
	Year                int          `json:"year" db:"year"`
	YearNumber          int          `json:"year_number" db:"year_number"`
	Species             nulls.String `json:"species" db:"species"`
	SpeciesClass        nulls.String `json:"species_class" db:"species_class"`
	SpeciesAGWGroup     nulls.String `json:"species_agw_group" db:"species_agw_group"`
	SpeciesSubsideGroup nulls.String `json:"species_subside_group" db:"species_subside_group"`
	SpeciesNativeStatus nulls.String `json:"species_native_status" db:"species_native_status"`
	Gender              nulls.String `json:"gender" db:"gender"`
	Cage                nulls.String `json:"cage" db:"cage"`
	Zone                nulls.String `json:"zone" db:"zone"`
	Ring                nulls.String `json:"ring" db:"ring"`
	AnimalType          nulls.String `json:"animal_type" db:"animal_type"`
	AnimalAge           nulls.String `json:"animal_age" db:"animal_age"`
	DiscoveryLocation   nulls.String `json:"discovery_location" db:"discovery_location"`
	DiscoveryDate       nulls.Time   `json:"discovery_date" db:"discovery_date"`
	DiscoveryCity       nulls.String `json:"discovery_city" db:"discovery_city"`
	DiscoveryPostalCode nulls.String `json:"discovery_postal_code" db:"discovery_postal_code"`
	EntryCause          nulls.String `json:"entry_cause" db:"entry_cause"`
	EntryCauseDetail    nulls.String `json:"entry_cause_detail" db:"entry_cause_detail"`
	EntryCauseNature    nulls.String `json:"entry_cause_nature" db:"entry_cause_nature"`
	CurrentStatus       string       `json:"current_status" db:"current_status"`
	IntakeDate          nulls.Time   `json:"intake_date" db:"intake_date"`
	IntakeGeneral       nulls.String `json:"intake_general" db:"intake_general"`
	IntakeWounds        nulls.String `json:"intake_wounds" db:"intake_wounds"`
	IntakeParasites     nulls.String `json:"intake_parasites" db:"intake_parasites"`
	IntakeRemarks       nulls.String `json:"intake_remarks" db:"intake_remarks"`
	OuttakeDate         nulls.Time   `json:"outtake_date" db:"outtake_date"`
	OuttakeType         nulls.String `json:"outtake_type" db:"outtake_type"`
	OuttakeLocation     nulls.String `json:"outtake_location" db:"outtake_location"`
	OuttakeRating       nulls.Int    `json:"outtake_rating" db:"outtake_rating"`
	OuttakeDead         nulls.Bool   `json:"outtake_dead" db:"outtake_dead"`
	Translations        nulls.String `json:"translations" db:"translations"`
	StateHash           nulls.String `json:"state_hash" db:"state_hash"`
	LastStateAt         nulls.Time   `json:"last_state_at" db:"last_state_at"`
	LastEventAt         time.Time    `json:"last_event_at" db:"last_event_at"`
	EventCount          int          `json:"event_count" db:"event_count"`
	CreatedAt           time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at" db:"updated_at"`
}

func (c ConsolidatedAnimal) String() string {
	jc, _ := json.Marshal(c)
	return string(jc)
}

// LocalizedField returns stored translation for field, falling back to canonical value.
func (c ConsolidatedAnimal) LocalizedField(lang, field string) string {
	canonical := ""
	switch field {
	case "species":
		canonical = c.Species.String
	case "animal_type":
		canonical = c.AnimalType.String
	case "animal_age":
		canonical = c.AnimalAge.String
	case "zone":
		canonical = c.Zone.String
	case "outtake_type":
		canonical = c.OuttakeType.String
	case "entry_cause":
		canonical = c.EntryCause.String
	case "entry_cause_detail":
		canonical = c.EntryCauseDetail.String
	case "entry_cause_nature":
		canonical = c.EntryCauseNature.String
	case "species_class":
		canonical = c.SpeciesClass.String
	case "species_agw_group":
		canonical = c.SpeciesAGWGroup.String
	case "species_subside_group":
		canonical = c.SpeciesSubsideGroup.String
	case "species_native_status":
		canonical = c.SpeciesNativeStatus.String
	case "outtake_rating":
		if c.OuttakeRating.Valid {
			canonical = strconv.Itoa(c.OuttakeRating.Int)
		}
	case "outtake_dead":
		if c.OuttakeDead.Valid {
			canonical = strconv.FormatBool(c.OuttakeDead.Bool)
		}
	}
	if !c.Translations.Valid {
		return canonical
	}
	var translations map[string]map[string]string
	if json.Unmarshal([]byte(c.Translations.String), &translations) == nil {
		if values, ok := translations[lang]; ok && values[field] != "" {
			return values[field]
		}
	}
	return canonical
}

type ConsolidatedAnimals []ConsolidatedAnimal

func (c ConsolidatedAnimals) String() string {
	jc, _ := json.Marshal(c)
	return string(jc)
}

func (c *ConsolidatedAnimal) Validate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

func (c *ConsolidatedAnimal) ValidateCreate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

func (c *ConsolidatedAnimal) ValidateUpdate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

func (c *ConsolidatedAnimal) applyState(payload EventPayload, eventTime time.Time) {
	// State events are snapshots: clear fields omitted by producer before applying.
	c.Species, c.Gender, c.Cage, c.Zone, c.Ring = nulls.String{}, nulls.String{}, nulls.String{}, nulls.String{}, nulls.String{}
	c.SpeciesClass, c.SpeciesAGWGroup, c.SpeciesSubsideGroup, c.SpeciesNativeStatus = nulls.String{}, nulls.String{}, nulls.String{}, nulls.String{}
	c.AnimalType, c.AnimalAge = nulls.String{}, nulls.String{}
	c.DiscoveryLocation, c.DiscoveryDate, c.DiscoveryCity, c.DiscoveryPostalCode = nulls.String{}, nulls.Time{}, nulls.String{}, nulls.String{}
	c.EntryCause, c.EntryCauseDetail, c.EntryCauseNature = nulls.String{}, nulls.String{}, nulls.String{}
	c.IntakeDate, c.IntakeGeneral, c.IntakeWounds, c.IntakeParasites, c.IntakeRemarks = nulls.Time{}, nulls.String{}, nulls.String{}, nulls.String{}, nulls.String{}
	c.OuttakeDate, c.OuttakeType, c.OuttakeLocation = nulls.Time{}, nulls.String{}, nulls.String{}
	c.OuttakeRating, c.OuttakeDead = nulls.Int{}, nulls.Bool{}
	previousCount := c.EventCount
	c.CurrentStatus = ""
	c.UpdateFromPayload(payload, EventTypeAnimalState, eventTime)
	c.CurrentStatus = payload.CurrentStatus
	if c.CurrentStatus == "" {
		c.CurrentStatus = payload.InitialStatus
	}
	c.EventCount = previousCount
	c.LastEventAt = eventTime
}

func nullableString(value string) nulls.String {
	if value == "" {
		return nulls.String{}
	}
	return nulls.NewString(value)
}

func (c *ConsolidatedAnimal) UpdateFromPayload(payload EventPayload, eventType EventType, eventTime time.Time) {
	// Update animal identification
	if payload.Animal.ID > 0 {
		c.AnimalID = payload.Animal.ID
	}
	if payload.Animal.Year > 0 {
		c.Year = payload.Animal.Year
	}
	if payload.Animal.YearNumber > 0 {
		c.YearNumber = payload.Animal.YearNumber
	}
	if payload.Animal.Species != "" {
		c.Species = nulls.NewString(payload.Animal.Species)
	}
	if payload.Animal.SpeciesClass != "" {
		c.SpeciesClass = nulls.NewString(payload.Animal.SpeciesClass)
	}
	if payload.Animal.SpeciesAGWGroup != "" {
		c.SpeciesAGWGroup = nulls.NewString(payload.Animal.SpeciesAGWGroup)
	}
	if payload.Animal.SpeciesSubsideGroup != "" {
		c.SpeciesSubsideGroup = nulls.NewString(payload.Animal.SpeciesSubsideGroup)
	}
	if payload.Animal.SpeciesNativeStatus != "" {
		c.SpeciesNativeStatus = nulls.NewString(payload.Animal.SpeciesNativeStatus)
	}
	if payload.Animal.Gender != "" {
		c.Gender = nulls.NewString(payload.Animal.Gender)
	}
	if payload.Animal.Cage != "" {
		c.Cage = nulls.NewString(payload.Animal.Cage)
	}
	if payload.Animal.Zone != "" {
		c.Zone = nulls.NewString(payload.Animal.Zone)
	}
	if payload.Animal.Ring != "" {
		c.Ring = nulls.NewString(payload.Animal.Ring)
	}
	if payload.Animal.AnimalType != "" {
		c.AnimalType = nulls.NewString(payload.Animal.AnimalType)
	}
	if payload.Animal.AnimalAge != "" {
		c.AnimalAge = nulls.NewString(payload.Animal.AnimalAge)
	}

	// Update discovery info
	if payload.Discovery.Location != "" {
		c.DiscoveryLocation = nulls.NewString(payload.Discovery.Location)
	}
	if payload.Discovery.Date != "" {
		if dt, err := time.Parse(DateTimeFormat, payload.Discovery.Date); err == nil {
			c.DiscoveryDate = nulls.NewTime(dt)
		}
	}
	if payload.Discovery.City != "" {
		c.DiscoveryCity = nulls.NewString(payload.Discovery.City)
	}
	if payload.Discovery.PostalCode != "" {
		c.DiscoveryPostalCode = nulls.NewString(payload.Discovery.PostalCode)
	}
	if payload.Discovery.EntryCause != "" {
		c.EntryCause = nulls.NewString(payload.Discovery.EntryCause)
	}
	if payload.Discovery.EntryCauseDetail != "" {
		c.EntryCauseDetail = nulls.NewString(payload.Discovery.EntryCauseDetail)
	}
	if payload.Discovery.EntryCauseNature != "" {
		c.EntryCauseNature = nulls.NewString(payload.Discovery.EntryCauseNature)
	}

	// Update intake info
	if payload.Intake.Date != "" {
		if dt, err := time.Parse(DateTimeFormat, payload.Intake.Date); err == nil {
			c.IntakeDate = nulls.NewTime(dt)
		}
	}
	if payload.Intake.General != "" {
		c.IntakeGeneral = nulls.NewString(payload.Intake.General)
	}
	if payload.Intake.Wounds != "" {
		c.IntakeWounds = nulls.NewString(payload.Intake.Wounds)
	}
	if payload.Intake.Parasites != "" {
		c.IntakeParasites = nulls.NewString(payload.Intake.Parasites)
	}
	if payload.Intake.Remarks != "" {
		c.IntakeRemarks = nulls.NewString(payload.Intake.Remarks)
	}

	// Update status based on event type
	switch eventType {
	case EventTypeAnimalDiscovered:
		c.CurrentStatus = payload.CurrentStatus
		if c.CurrentStatus == "" {
			c.CurrentStatus = "in_care"
		}
	case EventTypeAnimalStatusChanged:
		if payload.CurrentStatus != "" {
			c.CurrentStatus = payload.CurrentStatus
		}
	case EventTypeAnimalReleased:
		c.CurrentStatus = "released"
	case EventTypeAnimalDied:
		c.CurrentStatus = "died"
	}

	// Update outtake info
	if payload.Outtake.Date != "" {
		if dt, err := time.Parse(DateTimeFormat, payload.Outtake.Date); err == nil {
			c.OuttakeDate = nulls.NewTime(dt)
		}
	}
	if payload.Outtake.Type != "" {
		c.OuttakeType = nulls.NewString(payload.Outtake.Type)
	}
	if payload.Outtake.Location != "" {
		c.OuttakeLocation = nulls.NewString(payload.Outtake.Location)
	}
	if payload.Outtake.Rating != 0 {
		c.OuttakeRating = nulls.NewInt(payload.Outtake.Rating)
	}
	if payload.Outtake.Dead {
		c.OuttakeDead = nulls.NewBool(payload.Outtake.Dead)
	}

	// Update metadata
	c.LastEventAt = eventTime
	c.EventCount++
}

func (c *ConsolidatedAnimal) ApplyEvent(event EventStream) error {
	payload, err := event.GetPayload()
	if err != nil {
		return err
	}
	if event.EventType == EventTypeAnimalState {
		c.applyState(payload, event.CreatedAt)
		if payload.Translations == nil {
			c.Translations = nulls.String{}
		} else if encoded, marshalErr := json.Marshal(payload.Translations); marshalErr == nil {
			c.Translations = nulls.NewString(string(encoded))
		}
		if payload.StateHash != "" {
			c.StateHash = nulls.NewString(payload.StateHash)
		}
		c.LastStateAt = nulls.NewTime(event.CreatedAt)
		return nil
	}
	c.UpdateFromPayload(payload, event.EventType, event.CreatedAt)
	return nil
}
