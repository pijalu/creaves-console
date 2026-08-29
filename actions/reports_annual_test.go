//go:build sqlite
// +build sqlite

package actions

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"creaves-console/models"

	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/nulls"
	"github.com/gobuffalo/pop/v6"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAnnualTestApp builds a minimal Buffalo app injecting the shared testDB
// and mounting only the annual report routes.
func newAnnualTestApp(tx *pop.Connection) *buffalo.App {
	app := buffalo.New(buffalo.Options{Env: "test"})
	app.Use(func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			c.Set("tx", tx)
			return next(c)
		}
	})
	app.GET("/reports/annual", ReportsAnnualIndex)
	app.GET("/reports/annual/export.csv", ReportsAnnualExportCSV)
	return app
}

// annualFixture carries every field the 12 statistics tables read.
type annualFixture struct {
	instanceID    string
	animalID      int
	year          int
	species       string
	class         string
	agw           string
	subside       string
	nativeStatus  string
	age           string
	outtakeType   string
	outtakeRating *int
	outtakeDead   *bool
	entryCause    string
	entryDetail   string
	entryNature   string
}

func intPtr(i int) *int    { return &i }
func boolPtr(b bool) *bool { return &b }

// seedAnnualFixtures resets the tables and inserts known animals on two
// instances and two years. All expected counts derive from this set.
//
// Year 2024, center-a (6 animals, 2 with outtake):
//   - 3 Hérisson (1 NULL age, 1 "" age), 2 Chouette, 1 NULL species
//   - classes: Mammifère ×3 (a1 NULL class but species set — class bucket is
//     NULL → Unknown), Oiseau ×2, NULL ×1
//   - outtakes: a2 Transfert rating 1 dead=false, a3 Euthanasie rating -1 dead=true
//
// Year 2024, center-b (2 animals, 1 with outtake):
//   - Renard (outtake Transfert rating 0 dead=false), Chouette (no outtake)
//
// Year 2023, center-a: 1 Hérisson (no outtake) — proves year filtering.
func seedAnnualFixtures(t *testing.T, tx *pop.Connection) {
	t.Helper()
	require.NoError(t, tx.RawQuery("DELETE FROM consolidated_animals").Exec())
	require.NoError(t, tx.RawQuery("DELETE FROM creaves_instances").Exec())

	now := time.Now().UTC()
	for _, id := range []string{"center-a", "center-b"} {
		inst := &models.CreavesInstance{
			ID: uuid.Must(uuid.NewV4()), InstanceID: id, Name: "Name-" + id,
			FirstSeenAt: now, LastSeenAt: now,
		}
		require.NoError(t, tx.Create(inst))
	}

	fixtures := []annualFixture{
		// center-a 2024
		{instanceID: "center-a", animalID: 1, year: 2024, species: "Hérisson",
			agw: "Petits mammifères", subside: "G1", nativeStatus: "Indigène",
			entryCause: "Collision", entryDetail: "Collision / véhicule", entryNature: "Traumatisme"},
		{instanceID: "center-a", animalID: 2, year: 2024, species: "Hérisson", class: "Mammifère",
			agw: "Petits mammifères", subside: "G1", nativeStatus: "Indigène", age: "Adulte",
			outtakeType: "Transfert", outtakeRating: intPtr(1), outtakeDead: boolPtr(false),
			entryCause: "Collision", entryDetail: "Collision / véhicule", entryNature: "Traumatisme"},
		{instanceID: "center-a", animalID: 3, year: 2024, species: "Hérisson", class: "Mammifère",
			agw: "Petits mammifères", subside: "G1", nativeStatus: "Indigène", age: "",
			outtakeType: "Euthanasie", outtakeRating: intPtr(-1), outtakeDead: boolPtr(true),
			entryCause: "Collision", entryDetail: "Collision / animaux", entryNature: "Traumatisme"},
		{instanceID: "center-a", animalID: 4, year: 2024, species: "Chouette", class: "Oiseau",
			agw: "Rapaces", subside: "G2", nativeStatus: "Indigène", age: "Juvénile",
			entryCause: "Trouvé au sol", entryDetail: "Trouvé au sol / jardin", entryNature: "Orphelin"},
		{instanceID: "center-a", animalID: 5, year: 2024, species: "Chouette", class: "Oiseau",
			agw: "Rapaces", subside: "G2", nativeStatus: "Indigène", age: "Juvénile",
			entryCause: "Trouvé au sol", entryDetail: "Trouvé au sol / jardin", entryNature: "Orphelin"},
		{instanceID: "center-a", animalID: 6, year: 2024, // species NULL
			entryCause: "", entryNature: ""},
		// center-b 2024
		{instanceID: "center-b", animalID: 1, year: 2024, species: "Renard", class: "Mammifère",
			agw: "Petits mammifères", subside: "G1", nativeStatus: "Indigène", age: "Adulte",
			outtakeType: "Transfert", outtakeRating: intPtr(0), outtakeDead: boolPtr(false),
			entryCause: "Collision", entryDetail: "Collision / véhicule", entryNature: "Traumatisme"},
		{instanceID: "center-b", animalID: 2, year: 2024, species: "Chouette", class: "Oiseau",
			agw: "Rapaces", subside: "G2", nativeStatus: "Indigène", age: "Adulte",
			entryCause: "Trouvé au sol", entryDetail: "Trouvé au sol / route", entryNature: "Orphelin"},
		// center-a 2023
		{instanceID: "center-a", animalID: 7, year: 2023, species: "Hérisson", class: "Mammifère",
			agw: "Petits mammifères", subside: "G1", nativeStatus: "Indigène", age: "Adulte",
			entryCause: "Collision", entryDetail: "Collision / véhicule", entryNature: "Traumatisme"},
	}

	for _, f := range fixtures {
		a := &models.ConsolidatedAnimal{
			ID:            uuid.Must(uuid.NewV4()),
			InstanceID:    f.instanceID,
			AnimalID:      f.animalID,
			Year:          f.year,
			YearNumber:    f.animalID,
			CurrentStatus: "in_care",
			LastEventAt:   now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		setNulls := func(dst *nulls.String, v string) {
			if v != "" {
				*dst = nulls.NewString(v)
			}
		}
		setNulls(&a.Species, f.species)
		setNulls(&a.SpeciesClass, f.class)
		setNulls(&a.SpeciesAGWGroup, f.agw)
		setNulls(&a.SpeciesSubsideGroup, f.subside)
		setNulls(&a.SpeciesNativeStatus, f.nativeStatus)
		if f.age != "" {
			a.AnimalAge = nulls.NewString(f.age)
		} else if f.animalID == 3 && f.instanceID == "center-a" {
			a.AnimalAge = nulls.NewString("") // explicit empty string → Unknown
		}
		setNulls(&a.OuttakeType, f.outtakeType)
		if f.outtakeRating != nil {
			a.OuttakeRating = nulls.NewInt(*f.outtakeRating)
		}
		if f.outtakeDead != nil {
			a.OuttakeDead = nulls.NewBool(*f.outtakeDead)
		}
		setNulls(&a.EntryCause, f.entryCause)
		setNulls(&a.EntryCauseDetail, f.entryDetail)
		setNulls(&a.EntryCauseNature, f.entryNature)
		require.NoError(t, tx.Create(a))
	}
}

// sectionByID finds one computed section by ID.
func sectionByID(t *testing.T, sections []annualStatSection, id string) annualStatSection {
	t.Helper()
	for _, s := range sections {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("section %s not found", id)
	return annualStatSection{}
}

// countsByCategory maps category → (count, percent).
func countsByCategory(sec annualStatSection) map[string][2]string {
	m := make(map[string][2]string, len(sec.Rows))
	for _, r := range sec.Rows {
		m[r.Category] = [2]string{strconv.Itoa(r.Count), r.Percent}
	}
	return m
}

func TestAnnualStats_AllInstances_2024(t *testing.T) {
	seedAnnualFixtures(t, testDB)
	scope := ResolveReportScope("")

	sections, err := runAnnualStats(testDB, 2024, scope, "en-US")
	require.NoError(t, err)
	require.Len(t, sections, 12)

	// T = 8 animals on both instances in 2024.
	// Species: Hérisson 3, Chouette 3, Renard 1, Unknown 1 (a-6 species NULL).
	species := sectionByID(t, sections, "species")
	assert.Equal(t, 8, species.Total)
	sc := countsByCategory(species)
	assert.Equal(t, [2]string{"3", "37.5"}, sc["Hérisson"])
	assert.Equal(t, [2]string{"3", "37.5"}, sc["Chouette"])
	assert.Equal(t, [2]string{"1", "12.5"}, sc["Renard"])
	assert.Equal(t, [2]string{"1", "12.5"}, sc["Unknown"])

	// Class: Mammifère 3 (a2, a3, b1), Oiseau 3 (a4, a5, b2), Unknown 2 (a1, a6).
	class := sectionByID(t, sections, "class")
	assert.Equal(t, 8, class.Total)
	cc := countsByCategory(class)
	assert.Equal(t, [2]string{"3", "37.5"}, cc["Mammifère"])
	assert.Equal(t, [2]string{"3", "37.5"}, cc["Oiseau"])
	assert.Equal(t, [2]string{"2", "25.0"}, cc["Unknown"])

	// AGW: Petits mammifères 4, Rapaces 3, Unknown 1.
	agw := sectionByID(t, sections, "agw_group")
	assert.Equal(t, 8, agw.Total)
	ac := countsByCategory(agw)
	assert.Equal(t, [2]string{"4", "50.0"}, ac["Petits mammifères"])
	assert.Equal(t, [2]string{"3", "37.5"}, ac["Rapaces"])
	assert.Equal(t, [2]string{"1", "12.5"}, ac["Unknown"])

	// Subside: G1 4, G2 3, Unknown 1.
	sub := sectionByID(t, sections, "subsidies_group")
	assert.Equal(t, 8, sub.Total)
	sbc := countsByCategory(sub)
	assert.Equal(t, [2]string{"4", "50.0"}, sbc["G1"])
	assert.Equal(t, [2]string{"3", "37.5"}, sbc["G2"])
	assert.Equal(t, [2]string{"1", "12.5"}, sbc["Unknown"])

	// Native status: Indigène 7, Unknown 1.
	nat := sectionByID(t, sections, "native_status")
	assert.Equal(t, 8, nat.Total)
	nc := countsByCategory(nat)
	assert.Equal(t, [2]string{"7", "87.5"}, nc["Indigène"])
	assert.Equal(t, [2]string{"1", "12.5"}, nc["Unknown"])

	// Age: Adulte 2 (b1, b2), Juvénile 2, Unknown 4 (a1 NULL, a3 "", a6, + ... wait: a1, a3, a6 only = 3? No — count below).
	// a1 NULL, a3 "" → Unknown; a6 NULL → Unknown; a2 Adulte? No: a2 Adulte, a4/a5 Juvénile, b1 Adulte, b2 Adulte.
	// Adulte: a2, b1, b2 = 3; Juvénile: a4, a5 = 2; Unknown: a1, a3, a6 = 3.
	age := sectionByID(t, sections, "entry_age")
	assert.Equal(t, 8, age.Total)
	agc := countsByCategory(age)
	assert.Equal(t, [2]string{"3", "37.5"}, agc["Adulte"])
	assert.Equal(t, [2]string{"2", "25.0"}, agc["Juvénile"])
	assert.Equal(t, [2]string{"3", "37.5"}, agc["Unknown"])

	// Outtake type (only animals with outtake): Transfert 2 (a2, b1), Euthanasie 1 (a3).
	ot := sectionByID(t, sections, "outtake_type")
	assert.Equal(t, 3, ot.Total)
	otc := countsByCategory(ot)
	assert.Equal(t, [2]string{"2", "66.7"}, otc["Transfert"])
	assert.Equal(t, [2]string{"1", "33.3"}, otc["Euthanasie"])

	// Rating: Alive 1 (a2), Neutral 1 (b1), Dead 1 (a3).
	orating := sectionByID(t, sections, "outtake_rating")
	assert.Equal(t, 3, orating.Total)
	orc := countsByCategory(orating)
	assert.Equal(t, [2]string{"1", "33.3"}, orc["Alive"])
	assert.Equal(t, [2]string{"1", "33.3"}, orc["Neutral"])
	assert.Equal(t, [2]string{"1", "33.3"}, orc["Dead"])

	// Dead vs released: Released 2 (a2, b1), Dead 1 (a3).
	od := sectionByID(t, sections, "outtake_dead_released")
	assert.Equal(t, 3, od.Total)
	odc := countsByCategory(od)
	assert.Equal(t, [2]string{"2", "66.7"}, odc["Released"])
	assert.Equal(t, [2]string{"1", "33.3"}, odc["Dead"])

	// Entry cause: Collision 4 (a1, a2, a3, b1), Trouvé au sol 3 (a4, a5, b2), Unknown 1 (a6 "").
	ec := sectionByID(t, sections, "entry_cause")
	assert.Equal(t, 8, ec.Total)
	ecc := countsByCategory(ec)
	assert.Equal(t, [2]string{"4", "50.0"}, ecc["Collision"])
	assert.Equal(t, [2]string{"3", "37.5"}, ecc["Trouvé au sol"])
	assert.Equal(t, [2]string{"1", "12.5"}, ecc["Unknown"])

	// Detail: Collision / véhicule 3 (a1, a2, b1), Trouvé au sol / jardin 2,
	// Collision / animaux 1, Trouvé au sol / route 1, Unknown 1.
	ed := sectionByID(t, sections, "entry_cause_detail")
	assert.Equal(t, 8, ed.Total)
	edc := countsByCategory(ed)
	assert.Equal(t, [2]string{"3", "37.5"}, edc["Collision / véhicule"])
	assert.Equal(t, [2]string{"2", "25.0"}, edc["Trouvé au sol / jardin"])
	assert.Equal(t, [2]string{"1", "12.5"}, edc["Collision / animaux"])
	assert.Equal(t, [2]string{"1", "12.5"}, edc["Trouvé au sol / route"])
	assert.Equal(t, [2]string{"1", "12.5"}, edc["Unknown"])

	// Nature: Traumatisme 4, Orphelin 3, Unknown 1.
	en := sectionByID(t, sections, "entry_cause_nature")
	assert.Equal(t, 8, en.Total)
	enc := countsByCategory(en)
	assert.Equal(t, [2]string{"4", "50.0"}, enc["Traumatisme"])
	assert.Equal(t, [2]string{"3", "37.5"}, enc["Orphelin"])
	assert.Equal(t, [2]string{"1", "12.5"}, enc["Unknown"])
}

func TestAnnualStats_SingleInstance_ScopeSumsToGlobal(t *testing.T) {
	seedAnnualFixtures(t, testDB)

	global, err := runAnnualStats(testDB, 2024, ResolveReportScope(""), "en-US")
	require.NoError(t, err)
	a, err := runAnnualStats(testDB, 2024, ResolveReportScope("center-a"), "en-US")
	require.NoError(t, err)
	b, err := runAnnualStats(testDB, 2024, ResolveReportScope("center-b"), "en-US")
	require.NoError(t, err)

	// Totals differ per scope and sum to the global total for every table.
	for i := range global {
		ga, sa, sb := global[i].Total, a[i].Total, b[i].Total
		assert.Equal(t, ga, sa+sb, "section %s: global %d != a %d + b %d", global[i].ID, ga, sa, sb)
	}
	assert.Equal(t, 8, sectionByID(t, global, "species").Total)
	assert.Equal(t, 6, sectionByID(t, a, "species").Total)
	assert.Equal(t, 2, sectionByID(t, b, "species").Total)

	// Outtake tables: a has 2 outtakes, b has 1.
	assert.Equal(t, 2, sectionByID(t, a, "outtake_type").Total)
	assert.Equal(t, 1, sectionByID(t, b, "outtake_type").Total)

	// center-a species: Hérisson 3, Chouette 2, Unknown 1 → % of 6.
	ac := countsByCategory(sectionByID(t, a, "species"))
	assert.Equal(t, [2]string{"3", "50.0"}, ac["Hérisson"])
	assert.Equal(t, [2]string{"2", "33.3"}, ac["Chouette"])
	assert.Equal(t, [2]string{"1", "16.7"}, ac["Unknown"])
}

func TestAnnualStats_YearIsolation_AndEmptyYear(t *testing.T) {
	seedAnnualFixtures(t, testDB)

	sections, err := runAnnualStats(testDB, 2023, ResolveReportScope(""), "en-US")
	require.NoError(t, err)
	assert.Equal(t, 1, sectionByID(t, sections, "species").Total)
	assert.Equal(t, 0, sectionByID(t, sections, "outtake_type").Total)

	// Unknown year: all tables empty, total 0, percent 0.0 — no crash, no NaN.
	sections, err = runAnnualStats(testDB, 1999, ResolveReportScope(""), "en-US")
	require.NoError(t, err)
	for _, s := range sections {
		assert.Equal(t, 0, s.Total, "section %s", s.ID)
		assert.Empty(t, s.Rows, "section %s", s.ID)
	}
}

// getAnnual performs a GET against the test app and returns the recorder.
func getAnnual(t *testing.T, app *buffalo.App, path, lang string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest("GET", path, nil)
	require.NoError(t, err)
	if lang != "" {
		req.AddCookie(&http.Cookie{Name: "lang", Value: lang})
	}
	res := httptest.NewRecorder()
	app.ServeHTTP(res, req)
	return res
}

func TestReportsAnnualIndex_Handler(t *testing.T) {
	seedAnnualFixtures(t, testDB)
	app := newAnnualTestApp(testDB)

	// Default: latest year (2024), global scope.
	res := getAnnual(t, app, "/reports/annual", "")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	body := res.Body.String()
	assert.Contains(t, body, "Annual statistics")
	assert.Contains(t, body, "Top 20 species")
	assert.Contains(t, body, "Hérisson")
	// Both instances listed in the dropdown.
	assert.Contains(t, body, `value="center-a"`)
	assert.Contains(t, body, `value="center-b"`)
	// Export link preserves year + instance_id.
	assert.Contains(t, body, "/reports/annual/export.csv?year=2024")
	assert.Contains(t, body, "instance_id=")

	// Single instance scope renders and contains instance data.
	res = getAnnual(t, app, "/reports/annual?year=2024&instance_id=center-b", "")
	require.Equal(t, http.StatusOK, res.Code)
	assert.Contains(t, res.Body.String(), "Renard")

	// Unknown instance → 404.
	res = getAnnual(t, app, "/reports/annual?instance_id=nope", "")
	assert.Equal(t, http.StatusNotFound, res.Code)
}

func TestReportsAnnualExportCSV(t *testing.T) {
	seedAnnualFixtures(t, testDB)
	app := newAnnualTestApp(testDB)

	res := getAnnual(t, app, "/reports/annual/export.csv?year=2024", "")
	require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
	assert.Equal(t, "text/csv; charset=utf-8", res.Header().Get("Content-Type"))
	assert.Contains(t, res.Header().Get("Content-Disposition"), "reports-annual-2024.csv")

	// Strip UTF-8 BOM and parse.
	raw := res.Body.Bytes()
	require.True(t, len(raw) > 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF, "expected UTF-8 BOM")
	cr := csv.NewReader(strings.NewReader(string(raw[3:])))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	require.NoError(t, err)

	require.NotEmpty(t, records)
	assert.Equal(t, []string{"Year", "Instance", "Section", "Category", "Count", "%"}, records[0])

	// Collect (section, category) → count and verify a few known rows + totals.
	type key struct{ section, category string }
	got := map[key]string{}
	sectionsSeen := map[string]int{}
	for _, rec := range records[1:] {
		require.Len(t, rec, 6)
		assert.Equal(t, "2024", rec[0])
		assert.Equal(t, "All centers", rec[1])
		if rec[3] == "Total" {
			sectionsSeen[rec[2]]++
		}
		got[key{rec[2], rec[3]}] = rec[4]
	}
	assert.Equal(t, 12, len(sectionsSeen), "expected 12 section totals, got %v", sectionsSeen)
	assert.Equal(t, "8", got[key{"Top 20 species", "Total"}])
	assert.Equal(t, "3", got[key{"Top 20 species", "Hérisson"}])
	assert.Equal(t, "3", got[key{"Outtake by type", "Total"}])
	assert.Equal(t, "1", got[key{"Outtake: dead vs released", "Dead"}])

	// Single-instance export: filename carries the instance, rows scoped.
	res = getAnnual(t, app, "/reports/annual/export.csv?year=2024&instance_id=center-b", "")
	require.Equal(t, http.StatusOK, res.Code)
	assert.Contains(t, res.Header().Get("Content-Disposition"), "reports-annual-2024-center-b.csv")
	body := res.Body.String()
	assert.Contains(t, body, "center-b")
	assert.Contains(t, body, "Renard")
	assert.NotContains(t, body, "Hérisson")

	// French localization of headers + scope label.
	res = getAnnual(t, app, "/reports/annual/export.csv?year=2024", "fr")
	require.Equal(t, http.StatusOK, res.Code)
	body = res.Body.String()
	assert.Contains(t, body, "Année;Instance;Section;Catégorie;Nombre;%")
	assert.Contains(t, body, "Tous les centres")
	assert.Contains(t, body, "Top 20 espèces")
}
