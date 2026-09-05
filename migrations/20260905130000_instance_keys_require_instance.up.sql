-- Backfill: register instances for keys whose instance row was purged.
INSERT INTO creaves_instances (id, instance_id, name, description, first_seen_at, last_seen_at, created_at, updated_at)
SELECT UUID(), k.instance_id, k.name, '', NOW(), NOW(), NOW(), NOW()
FROM webhook_api_keys k
LEFT JOIN creaves_instances i ON i.instance_id = k.instance_id
WHERE k.instance_id IS NOT NULL AND k.instance_id <> '' AND i.id IS NULL;

-- Keys without any instance violate the new rule; remove them (console has
-- no production data, and such keys cannot authenticate a specific source).
DELETE FROM webhook_api_keys WHERE instance_id IS NULL OR instance_id = '';

ALTER TABLE webhook_api_keys MODIFY instance_id VARCHAR(255) NOT NULL;

ALTER TABLE webhook_api_keys
  ADD CONSTRAINT fk_webhook_api_keys_instance
  FOREIGN KEY (instance_id) REFERENCES creaves_instances(instance_id);
