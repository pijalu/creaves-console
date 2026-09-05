ALTER TABLE webhook_api_keys DROP FOREIGN KEY fk_webhook_api_keys_instance;

ALTER TABLE webhook_api_keys MODIFY instance_id VARCHAR(255) NULL DEFAULT NULL;
