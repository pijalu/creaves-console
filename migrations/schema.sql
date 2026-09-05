-- MySQL dump 10.13  Distrib 8.4.11, for macos26.6 (arm64)
--
-- Host: localhost    Database: consolidation
-- ------------------------------------------------------
-- Server version	8.4.11

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!50503 SET NAMES utf8mb4 */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;

--
-- Table structure for table `consolidated_animals`
--

DROP TABLE IF EXISTS `consolidated_animals`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `consolidated_animals` (
  `id` char(36) COLLATE utf8mb4_general_ci NOT NULL,
  `instance_id` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `animal_id` int NOT NULL,
  `year` int NOT NULL,
  `year_number` int NOT NULL,
  `species` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `animal_type` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `animal_age` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `discovery_location` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `discovery_date` datetime DEFAULT NULL,
  `current_status` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `intake_date` datetime DEFAULT NULL,
  `intake_general` text COLLATE utf8mb4_general_ci,
  `intake_wounds` text COLLATE utf8mb4_general_ci,
  `intake_parasites` text COLLATE utf8mb4_general_ci,
  `intake_remarks` text COLLATE utf8mb4_general_ci,
  `outtake_date` datetime DEFAULT NULL,
  `outtake_type` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `outtake_location` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `last_event_at` datetime NOT NULL,
  `event_count` int NOT NULL DEFAULT '0',
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  `gender` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `cage` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `zone` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `ring` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `discovery_city` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `discovery_postal_code` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `entry_cause` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `entry_cause_detail` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `entry_cause_nature` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `species_class` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `species_agw_group` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `species_subside_group` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `species_native_status` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `outtake_rating` int DEFAULT NULL,
  `outtake_dead` tinyint(1) DEFAULT NULL,
  `translations` json DEFAULT NULL,
  `state_hash` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `last_state_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `consolidated_animals_instance_id_animal_id_idx` (`instance_id`,`animal_id`),
  KEY `consolidated_animals_instance_id_idx` (`instance_id`),
  KEY `consolidated_animals_current_status_idx` (`current_status`),
  KEY `consolidated_animals_species_idx` (`species`),
  KEY `consolidated_animals_discovery_city_idx` (`discovery_city`),
  KEY `consolidated_animals_animal_type_idx` (`animal_type`),
  KEY `consolidated_animals_year_instance_id_idx` (`year`,`instance_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `creaves_instances`
--

DROP TABLE IF EXISTS `creaves_instances`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `creaves_instances` (
  `id` char(36) COLLATE utf8mb4_general_ci NOT NULL,
  `instance_id` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `name` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `description` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `first_seen_at` datetime DEFAULT NULL,
  `last_seen_at` datetime DEFAULT NULL,
  `last_event_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  `announced_expected_total` int DEFAULT NULL,
  `announced_expected_checksum` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `announced_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `creaves_instances_instance_id_idx` (`instance_id`),
  KEY `creaves_instances_last_seen_at_idx` (`last_seen_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `event_streams`
--

DROP TABLE IF EXISTS `event_streams`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `event_streams` (
  `id` char(36) COLLATE utf8mb4_general_ci NOT NULL,
  `instance_id` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `animal_id` int NOT NULL,
  `event_type` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `payload` json DEFAULT NULL,
  `source_db` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `imported_at` datetime NOT NULL,
  `processed_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  `resync_run_id` char(36) COLLATE utf8mb4_general_ci DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `event_streams_instance_id_animal_id_created_at_idx` (`instance_id`,`animal_id`,`created_at`),
  KEY `event_streams_processed_at_idx` (`processed_at`),
  KEY `event_streams_source_db_idx` (`source_db`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `import_runs`
--

DROP TABLE IF EXISTS `import_runs`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `import_runs` (
  `id` char(36) COLLATE utf8mb4_general_ci NOT NULL,
  `started_at` datetime NOT NULL,
  `completed_at` datetime DEFAULT NULL,
  `source_count` int NOT NULL DEFAULT '0',
  `events_imported` int NOT NULL DEFAULT '0',
  `events_processed` int NOT NULL DEFAULT '0',
  `status` varchar(255) COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'running',
  `error_message` text COLLATE utf8mb4_general_ci,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  PRIMARY KEY (`id`),
  KEY `import_runs_status_idx` (`status`),
  KEY `import_runs_started_at_idx` (`started_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `schema_migration`
--

DROP TABLE IF EXISTS `schema_migration`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `schema_migration` (
  `version` varchar(14) COLLATE utf8mb4_general_ci NOT NULL,
  PRIMARY KEY (`version`),
  UNIQUE KEY `schema_migration_version_idx` (`version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `users`
--

DROP TABLE IF EXISTS `users`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `users` (
  `id` char(36) COLLATE utf8mb4_general_ci NOT NULL,
  `login` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `name` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `email` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `admin` tinyint(1) NOT NULL DEFAULT '0',
  `active` tinyint(1) NOT NULL DEFAULT '1',
  `password_hash` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `users_login_idx` (`login`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Table structure for table `webhook_api_keys`
--

DROP TABLE IF EXISTS `webhook_api_keys`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `webhook_api_keys` (
  `id` char(36) COLLATE utf8mb4_general_ci NOT NULL,
  `name` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `key_hash` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `key_prefix` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `instance_id` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  `active` tinyint(1) NOT NULL DEFAULT '1',
  `last_used_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL,
  `updated_at` datetime NOT NULL,
  `key_value` varchar(255) COLLATE utf8mb4_general_ci DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `webhook_api_keys_key_hash_idx` (`key_hash`),
  KEY `webhook_api_keys_instance_id_idx` (`instance_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
/*!40101 SET character_set_client = @saved_cs_client */;
/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;

/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;

-- Dump completed on 2026-09-05  4:40:53
