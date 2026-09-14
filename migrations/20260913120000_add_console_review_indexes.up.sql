-- Performance review follow-up indexes (console read paths).
--
-- Snapshot report (reports/snapshot): WHERE intake_date ... AND (outtake_date ...)
-- had no supporting index at all -> full scan of consolidated_animals.
-- The OR condition across two columns lets MySQL use an index-merge union.
ALTER TABLE consolidated_animals ADD INDEX consolidated_animals_intake_date_idx (intake_date);
ALTER TABLE consolidated_animals ADD INDEX consolidated_animals_outtake_date_idx (outtake_date);

-- Annual report (reports/annual): WHERE year = ? GROUP BY <col> per section.
-- A (year, col) composite lets MySQL resolve the year slice and iterate
-- already-grouped values without a filesort for each of the 12 sections.
ALTER TABLE consolidated_animals ADD INDEX consolidated_animals_year_species_class_idx (year, species_class);
ALTER TABLE consolidated_animals ADD INDEX consolidated_animals_year_agw_group_idx (year, species_agw_group);
ALTER TABLE consolidated_animals ADD INDEX consolidated_animals_year_subside_group_idx (year, species_subside_group);
ALTER TABLE consolidated_animals ADD INDEX consolidated_animals_year_native_status_idx (year, species_native_status);
ALTER TABLE consolidated_animals ADD INDEX consolidated_animals_year_entry_cause_detail_idx (year, entry_cause_detail);
ALTER TABLE consolidated_animals ADD INDEX consolidated_animals_year_entry_cause_nature_idx (year, entry_cause_nature);
ALTER TABLE consolidated_animals ADD INDEX consolidated_animals_year_outtake_rating_idx (year, outtake_rating);

-- Sync checksums (sync_management): window function over the instance's
-- animal_state events ordered by created_at.
ALTER TABLE event_streams ADD INDEX event_streams_instance_type_created_idx (instance_id, event_type, created_at);
