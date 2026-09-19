-- BUG-R3: extended outtake fields + animal ready-for-release flag.
-- Additive contract extension on the webhook payload (all fields optional);
-- old producers simply omit them and the columns stay NULL.
ALTER TABLE consolidated_animals ADD COLUMN ready_for_release tinyint(1) DEFAULT NULL;
ALTER TABLE consolidated_animals ADD COLUMN outtake_precise_location varchar(255) DEFAULT NULL;
ALTER TABLE consolidated_animals ADD COLUMN outtake_stay_duration int DEFAULT NULL;
ALTER TABLE consolidated_animals ADD COLUMN outtake_corpse_destination varchar(255) DEFAULT NULL;
ALTER TABLE consolidated_animals ADD COLUMN outtake_corpse_destination_at datetime DEFAULT NULL;
