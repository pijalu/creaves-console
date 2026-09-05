package actions

// Shared stats source (bugs.md #8): every screen that shows "how many
// consolidated animals / received events exist" — dashboard, reports index,
// instance admin view, sync management — goes through these helpers so all
// screens agree for a given instant. Hand-rolled per-screen queries have
// drifted apart historically (dashboard 2763 vs reports 2852 vs instance
// page 2939 in the same audit session).

import (
	"creaves-console/models"

	"github.com/gobuffalo/pop/v6"
)

// CountConsolidatedAnimals returns the canonical number of consolidated
// animals for one instance, or for all instances when instanceID is empty.
func CountConsolidatedAnimals(tx *pop.Connection, instanceID string) (int, error) {
	if instanceID == "" {
		return tx.Count(&models.ConsolidatedAnimal{})
	}
	return tx.Where("instance_id = ?", instanceID).Count(&models.ConsolidatedAnimal{})
}

// CountEventStreams returns the canonical number of received events for one
// instance, or for all instances when instanceID is empty.
func CountEventStreams(tx *pop.Connection, instanceID string) (int, error) {
	if instanceID == "" {
		return tx.Count(&models.EventStream{})
	}
	return tx.Where("instance_id = ?", instanceID).Count(&models.EventStream{})
}
