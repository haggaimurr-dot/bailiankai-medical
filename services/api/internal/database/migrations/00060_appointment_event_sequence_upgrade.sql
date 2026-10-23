-- +goose Up
-- Repair databases that applied booking extensions before timeline sequencing
-- was added. Fresh databases already have the column from migration 29.
SET @arrival_event_upgrade_sql = IF(
 (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'appointment_events' AND column_name = 'sequence') = 0,
 'ALTER TABLE appointment_events ADD COLUMN sequence BIGINT UNSIGNED NOT NULL AUTO_INCREMENT UNIQUE',
 'SELECT 1'
);
PREPARE arrival_event_upgrade FROM @arrival_event_upgrade_sql;
EXECUTE arrival_event_upgrade;
DEALLOCATE PREPARE arrival_event_upgrade;

-- +goose Down
-- The column belongs to booking extensions; its migration owns removal.
SELECT 1;
