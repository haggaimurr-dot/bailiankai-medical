-- +goose Up
CREATE TABLE notification_backlog_settings (
 id INT PRIMARY KEY, max_pending INT NOT NULL DEFAULT 20,
 max_oldest_minutes INT NOT NULL DEFAULT 30, version INT NOT NULL DEFAULT 1,
 updated_by VARCHAR(64) NULL, updated_at DATETIME(3) NOT NULL
);
INSERT INTO notification_backlog_settings (id, max_pending, max_oldest_minutes, version, updated_at)
 VALUES (1, 20, 30, 1, UTC_TIMESTAMP(3));
-- +goose Down
DROP TABLE notification_backlog_settings;
