package actions

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"creaves-console/models"

	"github.com/gobuffalo/pop/v6"
)

// StateSetChecksum computes the shared sync fingerprint over a set of
// "<animal_id>|<state_hash>" lines: lines are sorted lexicographically,
// joined with "\n" and SHA-256 hashed, with a "sha256:" prefix. The identical
// formula is implemented on the creaves side (producer expected-set), so the
// two admin UIs can verify sync by comparing the printed checksums.
func StateSetChecksum(lines []string) string {
	sorted := make([]string, len(lines))
	copy(sorted, lines)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// InstanceSyncStatus is the per-instance sync verdict shown in the admin
// sync management view.
type InstanceSyncStatus struct {
	InstanceID string
	// ExpectedTotal counts distinct animals seen in ANY received event for
	// the instance — what the console should have consolidated.
	ExpectedTotal int
	// Confirmed counts animals whose latest received animal_state hash
	// matches the hash stored on their consolidated row.
	Confirmed int
	// Unconfirmed = ExpectedTotal - Confirmed: animals with a stale/missing
	// consolidated state (includes animals that never got a state event).
	Unconfirmed int
	// EventLogChecksum fingerprints the latest animal_state hash per animal
	// from the received event log.
	EventLogChecksum string
	// ConsolidatedChecksum fingerprints consolidated_animals.state_hash.
	ConsolidatedChecksum string
}

// checksum matches (true) only when both checksums are non-empty and equal.
func (s InstanceSyncStatus) ChecksumsMatch() bool {
	return s.EventLogChecksum != "" && s.EventLogChecksum == s.ConsolidatedChecksum
}

// ComputeInstanceSyncStatus derives the sync status of one instance from the
// console database. It loads state events and consolidated rows in Go rather
// than SQL JSON functions so the queries stay portable across SQLite (tests)
// and MySQL (production).
func ComputeInstanceSyncStatus(tx *pop.Connection, instanceID string) (*InstanceSyncStatus, error) {
	status := &InstanceSyncStatus{InstanceID: instanceID}

	// Distinct animals in the event log (any event type) = expected set.
	type animalRow struct {
		AnimalID int `db:"animal_id"`
	}
	expectedRows := []animalRow{}
	if err := tx.RawQuery(
		"SELECT DISTINCT animal_id FROM event_streams WHERE instance_id = ? ORDER BY animal_id",
		instanceID,
	).All(&expectedRows); err != nil {
		return nil, fmt.Errorf("failed to load expected animals: %w", err)
	}
	status.ExpectedTotal = len(expectedRows)

	// Latest animal_state hash per animal from the event log (created_at ASC,
	// later events overwrite earlier ones).
	events := &models.EventStreams{}
	if err := tx.Where("instance_id = ? AND event_type = ?", instanceID, models.EventTypeAnimalState).
		Order("created_at asc").All(events); err != nil {
		return nil, fmt.Errorf("failed to load state events: %w", err)
	}
	latest := map[int]string{}
	for i := range *events {
		payload, err := (*events)[i].GetPayload()
		if err != nil {
			continue // unreadable payload: cannot fingerprint this event
		}
		if payload.StateHash == "" {
			continue
		}
		latest[(*events)[i].AnimalID] = payload.StateHash
	}

	// Consolidated rows for the instance.
	animals := &models.ConsolidatedAnimals{}
	if err := tx.Where("instance_id = ?", instanceID).All(animals); err != nil {
		return nil, fmt.Errorf("failed to load consolidated animals: %w", err)
	}
	consolidatedHash := map[int]string{}
	for i := range *animals {
		a := &(*animals)[i]
		if a.StateHash.Valid && a.StateHash.String != "" {
			consolidatedHash[a.AnimalID] = a.StateHash.String
		}
	}

	// Confirmation: latest event hash must exist and equal the stored one.
	eventLines := make([]string, 0, len(latest))
	for animalID, hash := range latest {
		eventLines = append(eventLines, fmt.Sprintf("%d|%s", animalID, hash))
		if stored, ok := consolidatedHash[animalID]; ok && stored == hash {
			status.Confirmed++
		}
	}
	status.Unconfirmed = status.ExpectedTotal - status.Confirmed

	consolidatedLines := make([]string, 0, len(consolidatedHash))
	for animalID, hash := range consolidatedHash {
		consolidatedLines = append(consolidatedLines, fmt.Sprintf("%d|%s", animalID, hash))
	}

	status.EventLogChecksum = StateSetChecksum(eventLines)
	status.ConsolidatedChecksum = StateSetChecksum(consolidatedLines)
	return status, nil
}
