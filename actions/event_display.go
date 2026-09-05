package actions

// Event history presentation helpers (bugs.md #9): localized event type
// labels, badge classes and a meaningful Source column ("Resync <run id>"
// for events delivered by a producer resync run, "Live update" otherwise),
// plus the drill-down payload diff timeline.

import (
	"creaves-console/models"
	"fmt"
	"html/template"
	"strings"

	"github.com/gobuffalo/plush/v4"
)

// eventTypeLabels maps raw event type codes to localized UI labels.
var eventTypeLabels = map[string]map[string]string{
	"animal_discovered": {
		"en-US": "Discovered", "fr": "Découverte", "de": "Auffindung", "nl": "Vondst",
	},
	"animal_status_changed": {
		"en-US": "Status changed", "fr": "Changement de statut", "de": "Statusänderung", "nl": "Statuswijziging",
	},
	"animal_released": {
		"en-US": "Released", "fr": "Relâché", "de": "Freigelassen", "nl": "Vrijgelaten",
	},
	"animal_died": {
		"en-US": "Died", "fr": "Décédé", "de": "Verstorben", "nl": "Overleden",
	},
	"animal_state": {
		"en-US": "State snapshot", "fr": "Instantané d'état", "de": "Zustands-Snapshot", "nl": "Statussnapshot",
	},
}

// eventTypeBadgeClasses maps raw event type codes to Bootstrap badge classes.
var eventTypeBadgeClasses = map[string]string{
	"animal_discovered":     "info",
	"animal_status_changed": "warning",
	"animal_released":       "success",
	"animal_died":           "danger",
	"animal_state":          "secondary",
}

// eventSourceLabels holds the localized source names for non-resync events
// and for resync-delivered events (the latter get the shortened run id).
var eventSourceLabels = map[string]struct{ live, resync string }{
	"en-US": {live: "Live update", resync: "Resync"},
	"fr":    {live: "Mise à jour directe", resync: "Resynchronisation"},
	"de":    {live: "Live-Update", resync: "Resynchronisierung"},
	"nl":    {live: "Live update", resync: "Resync"},
}

// asEventStream normalizes a template argument to an EventStream value.
func asEventStream(v interface{}) (models.EventStream, bool) {
	switch e := v.(type) {
	case models.EventStream:
		return e, true
	case *models.EventStream:
		if e == nil {
			return models.EventStream{}, false
		}
		return *e, true
	}
	return models.EventStream{}, false
}

// eventTypeLabel is a template helper: localized label for the event type.
func eventTypeLabel(v interface{}, help plush.HelperContext) (string, error) {
	e, ok := asEventStream(v)
	if !ok {
		return "", fmt.Errorf("eventTypeLabel: expected models.EventStream, got %T", v)
	}
	if labels, ok := eventTypeLabels[e.EventType.String()]; ok {
		if label, ok := labels[currentUILang(help)]; ok {
			return label, nil
		}
		if label, ok := labels["en-US"]; ok {
			return label, nil
		}
	}
	return e.EventType.String(), nil
}

// eventTypeClass is a template helper: Bootstrap badge class for the event
// type.
func eventTypeClass(v interface{}, help plush.HelperContext) (template.HTML, error) {
	e, ok := asEventStream(v)
	if !ok {
		return template.HTML(""), fmt.Errorf("eventTypeClass: expected models.EventStream, got %T", v)
	}
	if class, ok := eventTypeBadgeClasses[e.EventType.String()]; ok {
		return template.HTML(class), nil
	}
	return template.HTML("secondary"), nil
}

// eventSource is a template helper: "Resync <short run id>" when the event
// was delivered by a resync run, "Live update" otherwise (bugs.md #9).
func eventSource(v interface{}, help plush.HelperContext) (string, error) {
	e, ok := asEventStream(v)
	if !ok {
		return "", fmt.Errorf("eventSource: expected models.EventStream, got %T", v)
	}
	lang := currentUILang(help)
	labels, ok := eventSourceLabels[lang]
	if !ok {
		labels = eventSourceLabels["en-US"]
	}
	if e.ResyncRunID != nil {
		return fmt.Sprintf("%s %s", labels.resync, e.ResyncRunID.String()[:8]), nil
	}
	return labels.live, nil
}

// timelineFieldLabels localizes the diff field labels of the drill-down
// timeline. Unknown fields fall back to a humanized key.
var timelineFieldLabels = map[string]map[string]string{
	"species": {
		"en-US": "Species", "fr": "Espèce", "de": "Art", "nl": "Soort",
	},
	"gender": {
		"en-US": "Gender", "fr": "Sexe", "de": "Geschlecht", "nl": "Geslacht",
	},
	"cage": {
		"en-US": "Cage", "fr": "Cage", "de": "Käfig", "nl": "Kooi",
	},
	"zone": {
		"en-US": "Zone", "fr": "Zone", "de": "Zone", "nl": "Zone",
	},
	"ring": {
		"en-US": "Ring", "fr": "Bague", "de": "Ring", "nl": "Ring",
	},
	"animal_type": {
		"en-US": "Type", "fr": "Type", "de": "Typ", "nl": "Type",
	},
	"animal_age": {
		"en-US": "Age", "fr": "Âge", "de": "Alter", "nl": "Leeftijd",
	},
	"discovery_city": {
		"en-US": "City", "fr": "Ville", "de": "Stadt", "nl": "Stad",
	},
	"entry_cause": {
		"en-US": "Entry cause", "fr": "Cause d'entrée", "de": "Fundursache", "nl": "Vangstreden",
	},
	"intake_date": {
		"en-US": "Intake date", "fr": "Date d'entrée", "de": "Aufnahmedatum", "nl": "Opnamedatum",
	},
	"outtake_type": {
		"en-US": "Outtake type", "fr": "Type de sortie", "de": "Abgabeart", "nl": "Afgifte type",
	},
	"outtake_date": {
		"en-US": "Outtake date", "fr": "Date de sortie", "de": "Abgabedatum", "nl": "Afgiftedatum",
	},
	"outtake_location": {
		"en-US": "Outtake location", "fr": "Lieu de sortie", "de": "Abgabeort", "nl": "Afgifteplaats",
	},
	"status": {
		"en-US": "Status", "fr": "Statut", "de": "Status", "nl": "Status",
	},
}

// fieldChange is one changed payload field in the drill-down timeline:
// Label is the localized field name, Old/New the canonical values
// (Old is empty for the animal's first event).
type fieldChange struct {
	Field string
	Old   string
	New   string
}

// timelineEntry is one event of the drill-down timeline with the changes it
// applied relative to the previous event.
type timelineEntry struct {
	Event  models.EventStream
	Change []fieldChange
}

// payloadField extracts the display value of one tracked field from a
// payload; empty when unset.
func payloadField(p models.EventPayload, field string) string {
	switch field {
	case "species":
		return p.Animal.Species
	case "gender":
		return p.Animal.Gender
	case "cage":
		return p.Animal.Cage
	case "zone":
		return p.Animal.Zone
	case "ring":
		return p.Animal.Ring
	case "animal_type":
		return p.Animal.AnimalType
	case "animal_age":
		return p.Animal.AnimalAge
	case "discovery_city":
		return p.Discovery.City
	case "entry_cause":
		return p.Discovery.EntryCause
	case "intake_date":
		return p.Intake.Date
	case "outtake_type":
		return p.Outtake.Type
	case "outtake_date":
		return p.Outtake.Date
	case "outtake_location":
		return p.Outtake.Location
	case "status":
		if p.CurrentStatus != "" {
			return p.CurrentStatus
		}
		return p.InitialStatus
	}
	return ""
}

// timelineFields lists the tracked payload fields in display order.
var timelineFields = []string{
	"species", "gender", "cage", "zone", "ring", "animal_type", "animal_age",
	"discovery_city", "entry_cause", "intake_date",
	"outtake_type", "outtake_date", "outtake_location", "status",
}

// localizedTimelineField returns the localized label for a tracked field.
func localizedTimelineField(field, lang string) string {
	if labels, ok := timelineFieldLabels[field]; ok {
		if label, ok := labels[lang]; ok && label != "" {
			return label
		}
		if label, ok := labels["en-US"]; ok && label != "" {
			return label
		}
	}
	return strings.Title(strings.ReplaceAll(field, "_", " "))
}

// buildEventTimeline turns the animal's event history into a timeline where
// each entry carries the field changes it applied relative to the previous
// event (the first entry shows the initial values). Parse failures degrade
// gracefully: the entry simply has no diff.
func buildEventTimeline(events *models.EventStreams, lang string) []timelineEntry {
	timeline := make([]timelineEntry, 0, len(*events))
	var prev models.EventPayload
	for i, e := range *events {
		entry := timelineEntry{Event: e}
		if p, err := e.GetPayload(); err == nil {
			for _, field := range timelineFields {
				newVal := payloadField(p, field)
				var oldVal string
				if i > 0 {
					oldVal = payloadField(prev, field)
				}
				if newVal != oldVal {
					entry.Change = append(entry.Change, fieldChange{
						Field: localizedTimelineField(field, lang),
						Old:   oldVal,
						New:   newVal,
					})
				}
			}
			prev = p
		}
		timeline = append(timeline, entry)
	}
	return timeline
}
