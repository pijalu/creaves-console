package models

import (
	"strconv"
	"strings"
	"time"

	"github.com/gobuffalo/pop/v6"
	"github.com/gobuffalo/validate/v3"
	"github.com/gofrs/uuid"
)

// ConsolidatedAnimalTombstone is the per-animal deletion marker backing the
// event processor's ordering guard (bugs.md BUG-8). Processing an
// animal_deleted event removes the animal's consolidated row AND its received
// events — without extra state, a STALE pre-destroy event redelivered later
// (full-redelivery recovery arrives in arbitrary batch order) would find no
// row and re-create the animal from years-old data ("resurrection").
//
// The tombstone records the deleted animal's newest deletion event time
// (EventAt): any state/transition event OLDER than EventAt is dropped for
// this (instance_id, animal_id); a NEWER event means the animal lives again
// on the producer and clears the tombstone.
//
// Tombstones deliberately live in their own table, NOT in event_streams: the
// sync checksums count distinct animals in the event log against the
// producer's expected set, which EXCLUDES destroyed animals — a marker row
// there would break the "matches producer" verdict.
//
// They are purged with everything else of an instance on
// InstanceCleanup (purgeInstance) and re-recorded naturally during the
// following full redelivery.
type ConsolidatedAnimalTombstone struct {
	ID         uuid.UUID `json:"id" db:"id"`
	InstanceID string    `json:"instance_id" db:"instance_id"`
	AnimalID   int       `json:"animal_id" db:"animal_id"`
	// EventAt is the event time (payload timestamp, falling back to the
	// event row's created_at) of the newest delete applied to this animal.
	EventAt   time.Time `json:"event_at" db:"event_at"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func (t ConsolidatedAnimalTombstone) String() string {
	return t.InstanceID + "/" + strconv.Itoa(t.AnimalID) + "@" + t.EventAt.String()
}

type ConsolidatedAnimalTombstones []ConsolidatedAnimalTombstone

func (t *ConsolidatedAnimalTombstone) Validate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

func (t *ConsolidatedAnimalTombstone) ValidateCreate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

func (t *ConsolidatedAnimalTombstone) ValidateUpdate(tx *pop.Connection) (*validate.Errors, error) {
	return validate.NewErrors(), nil
}

// FindTombstone loads the tombstone of one source animal; nil (no error)
// means the animal is not deleted.
func FindTombstone(tx *pop.Connection, instanceID string, animalID int) (*ConsolidatedAnimalTombstone, error) {
	tomb := &ConsolidatedAnimalTombstone{}
	err := tx.Where("instance_id = ? AND animal_id = ?", instanceID, animalID).First(tomb)
	if err != nil {
		return nil, err
	}
	return tomb, nil
}

// IsMissingTableErr reports whether err is the storage-level "table does not
// exist" failure for the tombstone table (MySQL "Error 1146 … doesn't exist",
// SQLite "no such table: …"). The processor tolerates it so a deployment
// where the new binary runs BEFORE `buffalo db migrate up` degrades to the
// pre-guard behavior (no tombstones) instead of failing every event of a
// not-yet-consolidated animal.
func IsMissingTableErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "consolidated_animal_tombstones") &&
		(strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "no such table"))
}
