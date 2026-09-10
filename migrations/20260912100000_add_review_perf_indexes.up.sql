ALTER TABLE consolidated_animals ADD INDEX year_desc_number_asc_idx (year DESC, year_number ASC);
ALTER TABLE consolidated_animals ADD INDEX consolidated_animals_discovery_postal_code_idx (discovery_postal_code);
ALTER TABLE event_streams ADD INDEX event_streams_instance_id_imported_at_idx (instance_id, imported_at);
