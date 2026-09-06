package excel

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSheetPosition(t *testing.T) {
	cases := []struct {
		name     string
		line     int
		col      int
		expected string
	}{
		{"first cell", 1, 1, "A1"},
		{"column Z boundary", 1, 26, "Z1"},
		{"column AA", 1, 27, "AA1"},
		{"column AG (registre width)", 1, 33, "AG1"},
		{"column N (stats width)", 1, 14, "N1"},
		{"column ZZ boundary", 1, 702, "ZZ1"},
		{"large line", 999, 1, "A999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, sheetPosition(tc.line, tc.col))
		})
	}
}

func TestGetQuery_Found(t *testing.T) {
	q, err := config.getQuery("registre_detail")
	require.NoError(t, err)
	assert.Equal(t, "registre.xlsx", q.Template)
	assert.Equal(t, "animals", q.Sheet)

	q, err = config.getQuery("STAT_COMMUNES")
	require.NoError(t, err)
	assert.Equal(t, "stats_communes.xlsx", q.Template)
	assert.Equal(t, "bdd", q.Sheet)
}

func TestGetQuery_Unknown(t *testing.T) {
	_, err := config.getQuery("does_not_exist")
	assert.Error(t, err)
}

func TestGetQueries_TwoConfigured(t *testing.T) {
	queries := GetQueries()
	require.Len(t, queries, 2)
	names := []string{queries[0].Name, queries[1].Name}
	assert.Contains(t, names, "registre_detail")
	assert.Contains(t, names, "stat_communes")
}

// The console queries must keep the exact French column aliases of the
// Creaves originals: the templates' pivot tables are bound to them.
func TestConfiguredQueries_ColumnAliases(t *testing.T) {
	registre, err := config.getQuery("registre_detail")
	require.NoError(t, err)
	assert.Equal(t, 33, strings.Count(registre.Query, ` AS "`),
		"registre_detail must expose the 33 columns of the registre.xlsx template")
	for _, alias := range []string{
		`AS "ID"`, `AS "année"`, `AS "N°"`, `AS "Espèce"`, `AS "Check"`,
		`AS "cage"`, `AS "Identification"`, `AS "Genre"`, `AS "Age"`,
		`AS "Date d'entrée"`, `AS "état général"`, `AS "Bléssures"`,
		`AS "Parasites"`, `AS "Remarque d'entrée"`, `AS "lieux de découverte"`,
		`AS "Cause de découverte"`, `AS "Date de découverte"`,
		`AS "Note sur la découverte"`, `AS "Date de sortie"`,
		`AS "Raison de la sortie"`, `AS "Lieux de relacher"`,
		`AS "Note sur la sortie"`, `AS "Prénom"`, `AS "Nom"`, `AS "Adresse"`,
		`AS "Ville"`, `AS "Pays"`, `AS "Adresse mail"`, `AS "Téléphone"`,
		`AS "Note"`, `AS "séjour"`, `AS "Nombre visite VT"`,
		`AS "KM parcouru pour l'animal"`,
	} {
		assert.Contains(t, registre.Query, alias)
	}

	stats, err := config.getQuery("stat_communes")
	require.NoError(t, err)
	assert.Equal(t, 14, strings.Count(stats.Query, ` AS "`),
		"stat_communes must expose the 14 columns of the stats_communes.xlsx template")
	for _, alias := range []string{
		`AS "Année"`, `AS "N°"`, `AS "Espèce"`, `AS "Date d'entrée"`,
		`AS "Cause de découverte"`, `AS "lieux de découverte"`, `AS "localté"`,
		`AS "Code postal"`, `AS "Commune"`, `AS "Province"`, `AS "Région"`,
		`AS "Pays"`, `AS "Cantonnement"`, `AS "Direction"`,
	} {
		assert.Contains(t, stats.Query, alias)
	}
}

func TestScopeSQL_Global(t *testing.T) {
	q := "SELECT 1 FROM consolidated_animals AS a {scopeWhere} ORDER BY a.year"
	sql, args := scopeSQL(q, "")
	assert.NotContains(t, sql, "{scopeWhere}")
	assert.NotContains(t, sql, "instance_id = ?")
	assert.Empty(t, args)
}

func TestScopeSQL_Instance(t *testing.T) {
	q := "SELECT 1 FROM consolidated_animals AS a {scopeWhere} ORDER BY a.year"
	sql, args := scopeSQL(q, "center-a")
	assert.Contains(t, sql, "WHERE a.instance_id = ?")
	assert.NotContains(t, sql, "{scopeWhere}")
	require.Len(t, args, 1)
	assert.Equal(t, "center-a", args[0])
}

func TestScopeSQL_ScopeAnd(t *testing.T) {
	q := "SELECT 1 FROM consolidated_animals AS a WHERE a.year = 2024 {scopeAnd}"
	sql, args := scopeSQL(q, "center-a")
	assert.Contains(t, sql, "AND a.instance_id = ?")
	assert.NotContains(t, sql, "{scopeAnd}")
	require.Len(t, args, 1)

	sql, args = scopeSQL(q, "")
	assert.NotContains(t, sql, "{scopeAnd}")
	assert.NotContains(t, sql, "AND a.instance_id")
	assert.Empty(t, args)
}
