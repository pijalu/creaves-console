package actions

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

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
	// from the received event log. Empty string = no state events received
	// ("no data", never a hash of an empty set).
	EventLogChecksum string
	// ConsolidatedChecksum fingerprints consolidated_animals.state_hash.
	// Empty string = no hashed consolidated rows stored ("no data").
	ConsolidatedChecksum string
	// NoData is true when neither an event-log fingerprint nor a
	// consolidated fingerprint can be computed. The UI must show a
	// "no data" state in that case — never a checksum and never "match".
	NoData bool
	// AnnouncedExpectedTotal / AnnouncedExpectedChecksum / AnnouncedAt
	// carry the producer's announcement (from the resync envelope "sync"
	// block, persisted on the instance row). AnnouncedExpectedTotal is nil
	// when the instance has not announced anything yet (no resync since
	// this feature shipped).
	AnnouncedExpectedTotal    *int
	AnnouncedExpectedChecksum string
	AnnouncedAt               *time.Time
}

// checksum matches (true) only when both checksums are non-empty and equal.
func (s InstanceSyncStatus) ChecksumsMatch() bool {
	return s.EventLogChecksum != "" && s.EventLogChecksum == s.ConsolidatedChecksum
}

// ChecksumMatchesAnnounced reports whether the stored-animal checksum equals
// the producer's announced expected checksum (both non-empty). This is THE
// completeness verdict: the announcement covers animals this console may
// never have received, so a match proves the transfer is complete.
func (s InstanceSyncStatus) ChecksumMatchesAnnounced() bool {
	return s.ConsolidatedChecksum != "" && s.AnnouncedExpectedChecksum != "" &&
		s.ConsolidatedChecksum == s.AnnouncedExpectedChecksum
}

// HasAnnouncement reports whether the instance ever announced its expected
// sync state (i.e. a full resync ran since the announcement feature shipped).
func (s InstanceSyncStatus) HasAnnouncement() bool {
	return s.AnnouncedExpectedTotal != nil && s.AnnouncedExpectedChecksum != ""
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

	latest, err := latestEventStateHashes(tx, instanceID)
	if err != nil {
		return nil, err
	}
	consolidatedHash, err := consolidatedStateHashes(tx, instanceID)
	if err != nil {
		return nil, err
	}
	loadAnnouncedSyncStatus(tx, instanceID, status)

	// Confirmation: latest event hash must exist and equal the stored one.
	eventLines := checksumLines(latest)
	for animalID, hash := range latest {
		if stored, ok := consolidatedHash[animalID]; ok && stored == hash {
			status.Confirmed++
		}
	}
	status.Unconfirmed = status.ExpectedTotal - status.Confirmed
	consolidatedLines := checksumLines(consolidatedHash)

	// Empty sets MUST NOT produce the SHA-256 of the empty string (the
	// historic false "checksum match"): an empty fingerprint is "no data".
	status.NoData = len(latest) == 0 && len(consolidatedHash) == 0
	if len(latest) > 0 {
		status.EventLogChecksum = StateSetChecksum(eventLines)
	}
	if len(consolidatedHash) > 0 {
		status.ConsolidatedChecksum = StateSetChecksum(consolidatedLines)
	}
	return status, nil
}

// latestEventStateHashes maps each animal to the state hash of its latest
// animal_state event (created_at ASC, later events overwrite earlier ones).
func latestEventStateHashes(tx *pop.Connection, instanceID string) (map[int]string, error) {
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
	return latest, nil
}

// consolidatedStateHashes maps each consolidated animal of the instance to
// its stored state hash (animals without a hash are omitted).
func consolidatedStateHashes(tx *pop.Connection, instanceID string) (map[int]string, error) {
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
	return consolidatedHash, nil
}

// loadAnnouncedSyncStatus copies the producer announcement from the instance
// row into the status (a missing instance row simply leaves it empty).
func loadAnnouncedSyncStatus(tx *pop.Connection, instanceID string, status *InstanceSyncStatus) {
	instance := &models.CreavesInstance{}
	if err := tx.Where("instance_id = ?", instanceID).First(instance); err != nil {
		return
	}
	if instance.AnnouncedExpectedTotal.Valid {
		total := instance.AnnouncedExpectedTotal.Int
		status.AnnouncedExpectedTotal = &total
	}
	if instance.AnnouncedExpectedChecksum.Valid {
		status.AnnouncedExpectedChecksum = instance.AnnouncedExpectedChecksum.String
	}
	if instance.AnnouncedAt.Valid {
		at := instance.AnnouncedAt.Time
		status.AnnouncedAt = &at
	}
}

// checksumLines renders an animal→hash map as "<id>|<hash>" lines.
func checksumLines(hashes map[int]string) []string {
	lines := make([]string, 0, len(hashes))
	for animalID, hash := range hashes {
		lines = append(lines, fmt.Sprintf("%d|%s", animalID, hash))
	}
	return lines
}
