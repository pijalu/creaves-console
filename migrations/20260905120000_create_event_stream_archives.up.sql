CREATE TABLE event_stream_archives (
  id CHAR(36) PRIMARY KEY,
  scope VARCHAR(16) NOT NULL,
  instance_id VARCHAR(255) NULL,
  event_count INTEGER NOT NULL,
  content LONGTEXT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);
CREATE INDEX event_stream_archives_created_at_idx ON event_stream_archives (created_at);
