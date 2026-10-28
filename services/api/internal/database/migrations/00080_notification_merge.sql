-- +goose Up
CREATE TABLE notification_merge_settings (
 id INT PRIMARY KEY, merge_window_minutes INT NOT NULL DEFAULT 60,
 merge_enabled BOOLEAN NOT NULL DEFAULT TRUE, version INT NOT NULL DEFAULT 1,
 updated_by VARCHAR(64) NULL, updated_at DATETIME(3) NOT NULL
);
INSERT INTO notification_merge_settings (id, merge_window_minutes, merge_enabled, version, updated_at) VALUES (1, 60, TRUE, 1, UTC_TIMESTAMP(3));
ALTER TABLE notifications ADD COLUMN urgent BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE notifications ADD COLUMN merged_into VARCHAR(64) NULL;
ALTER TABLE notifications ADD INDEX notification_merge (patient_id, status, send_at);
-- +goose Down
ALTER TABLE notifications DROP INDEX notification_merge;
ALTER TABLE notifications DROP COLUMN merged_into;
ALTER TABLE notifications DROP COLUMN urgent;
DROP TABLE notification_merge_settings;
