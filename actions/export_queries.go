package actions

import "strings"

// Ported Creaves export queries (bugs.md item 3) running against the
// denormalized consolidated_animals table. Only reports whose source data is
// consolidated are ported; the rest are documented as skipped in
// docs/plans/item3-console-export-reports.md.
//
// Placeholders substituted by buildExportSQL:
//
//	{scopeWhere}  "WHERE a.instance_id = ?" (instance scope) or "" (global)
//	{scopeAnd}    "AND a.instance_id = ?"   (instance scope) or "" (global)
//	{df}          date-format fn: (col, fmt) -> dialect-specific DATE_FORMAT/strftime
//	{year}        year extraction: (col) -> EXTRACT/YEAR or strftime('%Y', col)
//	{stay}        days-in-care expression (outtake - intake + 1)
//
// "a" is always the consolidated_animals alias.

// exportQuery is one ported report.
type exportQuery struct {
	Name        string
	Description string
	// SQL with the placeholders above.
	SQL string
	// True when the report is aggregated (GROUP BY) — the online view then
	// disables row-level filters that make no sense on aggregates.
	Aggregate bool
}

// exportQueries is the registry of ported reports, keyed by stable id.
var exportQueries = []exportQuery{
	{
		Name: "register", Description: "Registre",
		SQL: `SELECT DISTINCT
			a.year AS "Année", a.year_number AS "N°", a.species AS "Espèce",
			a.ring AS "Identification",
			{df:intake_date:%d/%m/%Y} AS "Date d'entrée",
			a.discovery_location AS "Lieux / Adresse de découverte",
			a.discovery_postal_code AS "Code postal découverte",
			a.discovery_city AS "Ville découverte",
			a.entry_cause AS "Cause de découverte",
			a.outtake_type AS "Raison de la sortie",
			{df:outtake_date:%d/%m/%Y} AS "Date de sortie",
			a.outtake_location AS "Lieux de la sortie",
			a.instance_id AS "Instance"
			FROM consolidated_animals AS a
			{scopeWhere}
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "detail_register", Description: "Registre détaillé",
		SQL: `SELECT
			a.year AS "Année", a.animal_id AS "ID", a.year_number AS "N°",
			a.species AS "Espèce", a.animal_type AS "Type", a.cage AS "cage",
			a.zone AS "zone", a.ring AS "Identification", a.gender AS "Genre",
			a.animal_age AS "Age",
			{df:intake_date:%d/%m/%Y} AS "Date d'entrée",
			a.intake_general AS "état général", a.intake_wounds AS "Bléssures",
			a.intake_parasites AS "Parasites", a.intake_remarks AS "Remarque d'entrée",
			a.discovery_city AS "Ville de découverte",
			a.discovery_postal_code AS "Code postal de découverte",
			a.discovery_location AS "lieux / Adresse de découverte",
			a.entry_cause AS "Cause de découverte principale",
			a.entry_cause_detail AS "Cause de découverte détaillée",
			a.entry_cause_nature AS "Nature de la cause d'entrée",
			{df:discovery_date:%d/%m/%Y} AS "Date de découverte",
			{df:outtake_date:%d/%m/%Y} AS "Date de sortie",
			{stay} AS "séjour",
			a.outtake_type AS "Raison de la sortie",
			a.outtake_location AS "Lieux de relacher",
			a.discoverer_firstname AS "Prénom", a.discoverer_lastname AS "Nom",
			a.discoverer_address AS "Adresse", a.discoverer_postal_code AS "Code postal",
			a.discoverer_city AS "Ville", a.discoverer_country AS "Pays",
			a.discoverer_email AS "E mail", a.discoverer_phone AS "Téléphone",
			a.discoverer_note AS "Note",
			a.instance_id AS "Instance"
			FROM consolidated_animals AS a
			{scopeWhere}
			ORDER BY 1 DESC, 2 ASC, 3 ASC`,
	},
	{
		Name: "dead_register", Description: "Registre des cadavres",
		SQL: `SELECT
			a.year AS "Année", a.year_number AS "N°", a.species AS "Espèce",
			a.ring AS "Identification",
			{df:intake_date:%d/%m/%Y} AS "Date d'entrée",
			a.outtake_type AS "Raison de la sortie",
			{df:outtake_date:%d/%m/%Y} AS "Date de sortie",
			a.instance_id AS "Instance"
			FROM consolidated_animals AS a
			WHERE a.outtake_dead = {true} {scopeAnd}
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "descoverer_register", Description: "Registre des découvreurs",
		SQL: `SELECT
			a.year AS "Année", a.animal_id AS "ID", a.year_number AS "numéro annuel",
			a.species AS "espèce",
			{df:intake_date:%d/%m/%Y} AS "Date d'entrée",
			a.outtake_type AS "raison de sortie",
			a.discoverer_firstname AS "Prénom", a.discoverer_lastname AS "Nom",
			a.discoverer_address AS "adresse", a.discoverer_city AS "ville",
			a.discoverer_email AS "mail", a.discoverer_phone AS "téléphone",
			a.discoverer_donation AS "Don", a.discoverer_note AS "Note sur la découverte",
			a.instance_id AS "Instance"
			FROM consolidated_animals AS a
			WHERE a.discoverer_lastname IS NOT NULL AND a.discoverer_lastname <> '' {scopeAnd}
			ORDER BY 1 DESC, 2 ASC, 3 ASC`,
	},
	{
		Name: "donation_register", Description: "Registre des dons",
		SQL: `SELECT
			a.year AS "Année",
			a.discoverer_firstname AS "Prénom", a.discoverer_lastname AS "Nom",
			a.discoverer_address AS "Adresse", a.discoverer_postal_code AS "Code postal",
			a.discoverer_city AS "ville", a.discoverer_email AS "mail",
			a.discoverer_phone AS "Téléphone", a.discoverer_donation AS "Don",
			a.instance_id AS "Instance"
			FROM consolidated_animals AS a
			WHERE a.discoverer_donation IS NOT NULL AND a.discoverer_donation <> ''
				AND a.discoverer_donation <> '0' {scopeAnd}
			ORDER BY 1 DESC, 2 ASC, 3 ASC`,
	},
	{
		Name: "species_registre", Description: "Registre détaillé des espèces",
		SQL: `SELECT DISTINCT
			a.year AS "Année", a.year_number AS "N°", a.species AS "Espèce",
			a.species_family AS "Famille", a.species_order AS "Ordre",
			a.species_class AS "Classe", a.species_agw_group AS "CREAVES Groupes",
			CASE WHEN a.species_game = {true} THEN 'Gibier' ELSE '' END AS "Gibier",
			CASE WHEN a.species_huntable = {true} THEN 'Chassable' ELSE '' END AS "Chassable",
			a.discovery_location AS "lieux de découverte",
			{df:intake_date:%d/%m/%Y} AS "Date d'entrée",
			a.entry_cause AS "Cause de découverte",
			a.outtake_type AS "Raison de la sortie",
			{df:outtake_date:%d/%m/%Y} AS "Date de sortie",
			a.instance_id AS "Instance"
			FROM consolidated_animals AS a
			{scopeWhere}
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "entry_age", Description: "Age à l'entrée", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.animal_age AS "Age à l'Entrée", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1, a.animal_age
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "sortie_reason", Description: "Causes de sortie", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.outtake_type AS "Causes de sortie", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			WHERE a.outtake_type IS NOT NULL AND a.outtake_type <> '' {scopeAnd}
			GROUP BY 1, a.outtake_type
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "sortie_types", Description: "Types de sortie", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année",
			CASE
				WHEN a.outtake_rating = -1 THEN 'Animal sorti Mort'
				WHEN a.outtake_rating = 0 THEN 'La sortie est Neutre'
				WHEN a.outtake_rating = 1 THEN 'Animal sorti Vivant'
			END AS "Type de sortie",
			COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			WHERE a.outtake_rating IS NOT NULL {scopeAnd}
			GROUP BY 1, a.outtake_rating
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "animals_species", Description: "Nombre d'animaux accueillis selon l'espèce", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.species AS "Espèce", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1, a.species
			ORDER BY 1 DESC, 3 DESC`,
	},
	{
		Name: "AGW_group", Description: "Nombre d'animaux accueillis selon le groupe du SPW CREAVES", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.species_agw_group AS "CREAVES Groupe", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1, a.species_agw_group
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "animals_types", Description: "Nombre d'animaux accueillis selon le type", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.animal_type AS "Type", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1, a.animal_type
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "animals_family", Description: "Nombre d'animaux accueillis selon la famille", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.species_family AS "Famille", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1, a.species_family
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "animals_order", Description: "Nombre d'animaux accueillis selon l'ordre", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.species_order AS "Ordre", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1, a.species_order
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "animals_class", Description: "Nombre d'animaux accueillis selon la classe", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.species_class AS "Classe", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1, a.species_class
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "animals_game", Description: "Espèce gibier accueillie", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.species AS "Espèce", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			WHERE a.species_game = {true} {scopeAnd}
			GROUP BY 1, a.species
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "animals_huntable", Description: "Espèce chassable accueillie", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.species AS "Espèce", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			WHERE a.species_huntable = {true} {scopeAnd}
			GROUP BY 1, a.species
			ORDER BY 1 DESC, 2 ASC`,
	},
	{
		Name: "native_status", Description: "Statut d'indigénat", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.species_native_status AS "Statut", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1, a.species_native_status
			ORDER BY 1 DESC, 3 DESC`,
	},
	{
		Name: "entry_causes", Description: "Causes d'entrée", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.entry_cause AS "Cause", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1, a.entry_cause
			ORDER BY 1 DESC, 3 DESC`,
	},
	{
		Name: "entry_causes_detail", Description: "Causes d'entrée détail", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.entry_cause_nature AS "Nature de la cause",
			a.entry_cause AS "Cause", a.entry_cause_detail AS "Cause détail",
			COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1, a.entry_cause_nature, a.entry_cause, a.entry_cause_detail
			ORDER BY 1 DESC, 5 DESC`,
	},
	{
		Name: "nature_entry_causes", Description: "Nature des causes d'entrée", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", a.entry_cause_nature AS "Nature des causes d'entrée",
			COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1, a.entry_cause_nature
			ORDER BY 1 DESC, 3 DESC`,
	},
	{
		Name: "nombre", Description: "Nombre d'animaux dans l'année", Aggregate: true,
		SQL: `SELECT
			a.year AS "Année", COUNT(*) AS "Nombre"
			FROM consolidated_animals AS a
			{scopeWhere}
			GROUP BY 1
			ORDER BY 1 DESC`,
	},
}

// findExportQuery returns the report with the given id (case-insensitive).
func findExportQuery(id string) *exportQuery {
	for i := range exportQueries {
		if strings.EqualFold(exportQueries[i].Name, id) {
			return &exportQueries[i]
		}
	}
	return nil
}
