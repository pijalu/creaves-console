package actions

import (
	"net/http/httptest"
	"testing"

	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/plush/v4"
	"github.com/stretchr/testify/assert"

	"creaves-console/models"
)

// TestConsolidatedSortClausesPinsDefaultOrdering pins the default (no/unknown
// sort param) ordering: the historical register order. A bad link must never
// be able to alter — let alone inject — ordering SQL.
func TestConsolidatedSortClausesPinsDefaultOrdering(t *testing.T) {
	for _, sortKey := range []string{"", "bogus", "species; DROP TABLE consolidated_animals"} {
		assert.Equal(t,
			[]string{"year desc", "year_number asc"},
			consolidatedSortClauses(sortKey, ""), "sort=%q", sortKey)
	}
}

// TestConsolidatedSortClausesPinsWhitelistAndDirection pins whitelist mapping,
// direction handling and the stable tail keys.
func TestConsolidatedSortClausesPinsWhitelistAndDirection(t *testing.T) {
	assert.Equal(t,
		[]string{"species desc", "year desc", "year_number asc"},
		consolidatedSortClauses("species", "desc"))

	// invalid dir falls back to asc
	assert.Equal(t,
		[]string{"species asc", "year desc", "year_number asc"},
		consolidatedSortClauses("species", "DROP"))

	assert.Equal(t,
		[]string{"instance_id asc", "year desc", "year_number asc"},
		consolidatedSortClauses("instance", "asc"))

	assert.Equal(t,
		[]string{"outtake_date desc", "year desc", "year_number asc"},
		consolidatedSortClauses("outcome_date", "desc"))
}

// TestOutcomeStatusPinsClassification pins the positive/neutral/negative
// outcome classification from the stored rating/dead values.
func TestOutcomeStatusPinsClassification(t *testing.T) {
	build := func(rating nulls.Int, dead nulls.Bool) *models.ConsolidatedAnimal {
		return &models.ConsolidatedAnimal{OuttakeRating: rating, OuttakeDead: dead}
	}

	assert.Equal(t, "negative", build(nulls.NewInt(-1), nulls.Bool{}).OutcomeStatus())
	assert.Equal(t, "negative", build(nulls.Int{}, nulls.NewBool(true)).OutcomeStatus())
	// dead=true wins even over a positive rating
	assert.Equal(t, "negative", build(nulls.NewInt(1), nulls.NewBool(true)).OutcomeStatus())
	assert.Equal(t, "positive", build(nulls.NewInt(1), nulls.Bool{}).OutcomeStatus())
	assert.Equal(t, "positive", build(nulls.NewInt(1), nulls.NewBool(false)).OutcomeStatus())
	// explicit neutral: rating 0 crosses the wire since the omitempty drop
	assert.Equal(t, "neutral", build(nulls.NewInt(0), nulls.Bool{}).OutcomeStatus())
	assert.Equal(t, "neutral", build(nulls.NewInt(0), nulls.NewBool(false)).OutcomeStatus())
	// no information at all: unknown
	assert.Equal(t, "", build(nulls.Int{}, nulls.Bool{}).OutcomeStatus())
}

// TestConsolidatedSortHelpersRender pins the header-link helper behaviour on
// the console register (toggle, filter preservation, page reset).
func TestConsolidatedSortHelpersRender(t *testing.T) {
	build := func(target string) plush.HelperContext {
		req := httptest.NewRequest("GET", target, nil)
		return plush.HelperContext{Context: plush.NewContextWith(map[string]interface{}{"request": req})}
	}

	href, err := sortLink("species", build("/consolidated_animals?year=2024&page=2"))
	if err != nil {
		t.Fatalf("sortLink: %v", err)
	}
	if want := "/consolidated_animals?dir=asc&sort=species&year=2024"; string(href) != want {
		t.Fatalf("sortLink inactive = %q, want %q", string(href), want)
	}

	href, err = sortLink("species", build("/consolidated_animals?sort=species&dir=asc"))
	if err != nil {
		t.Fatalf("sortLink: %v", err)
	}
	if want := "/consolidated_animals?dir=desc&sort=species"; string(href) != want {
		t.Fatalf("sortLink toggle = %q, want %q", string(href), want)
	}

	icon, err := sortIcon("species", build("/consolidated_animals?sort=species&dir=desc"))
	if err != nil {
		t.Fatalf("sortIcon: %v", err)
	}
	if string(icon) != "▼" {
		t.Fatalf("sortIcon desc = %q, want ▼", string(icon))
	}
}

// TestOutcomeHelpersAcceptValueAndPointer pins that outcomeClass/outcomeLabel
// accept both the value form (index loop variable) and the pointer form
// (show handler context), and reject unrelated types with an error instead of
// rendering a wrong badge.
func TestOutcomeHelpersAcceptValueAndPointer(t *testing.T) {
	help := plush.HelperContext{Context: plush.NewContextWith(map[string]interface{}{})}

	dead := models.ConsolidatedAnimal{OuttakeDead: nulls.NewBool(true)}
	positive := models.ConsolidatedAnimal{OuttakeRating: nulls.NewInt(2)}

	for _, a := range []models.ConsolidatedAnimal{dead, positive} {
		for _, v := range []interface{}{a, &a} {
			cls, err := outcomeClass(v, help)
			if err != nil {
				t.Fatalf("outcomeClass(%T): %v", v, err)
			}
			want := "danger"
			if a.OuttakeRating.Int == 2 {
				want = "success"
			}
			if string(cls) != want {
				t.Fatalf("outcomeClass = %q, want %q", string(cls), want)
			}

			lbl, err := outcomeLabel(v, help)
			if err != nil {
				t.Fatalf("outcomeLabel(%T): %v", v, err)
			}
			wantLbl := "Negative"
			if a.OuttakeRating.Int == 2 {
				wantLbl = "Positive"
			}
			if lbl != wantLbl {
				t.Fatalf("outcomeLabel = %q, want %q", lbl, wantLbl)
			}
		}
	}

	if _, err := outcomeClass("bogus", help); err == nil {
		t.Fatal("outcomeClass(bogus): expected error for non-animal argument")
	}
	if _, err := outcomeLabel("bogus", help); err == nil {
		t.Fatal("outcomeLabel(bogus): expected error for non-animal argument")
	}
}
