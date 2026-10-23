-- +goose Up
-- Older local schemas applied version 43 before its unique key was added.
-- Preserve all duplicate rows for audit, keep the original task identity and
-- copy the latest contact state before repairing the constraint.
CREATE TABLE schedule_patient_task_repair_archive AS
SELECT tasks.*, CURRENT_TIMESTAMP(3) AS archived_at
FROM schedule_patient_tasks tasks
WHERE EXISTS (SELECT 1 FROM schedule_patient_tasks other
 WHERE other.appointment_id = tasks.appointment_id AND other.slot_id = tasks.slot_id AND other.id <> tasks.id);
CREATE TEMPORARY TABLE schedule_task_repair AS
SELECT id, appointment_id, slot_id, reason, contact_status, note, updated_by, updated_at,
 FIRST_VALUE(id) OVER (PARTITION BY appointment_id, slot_id ORDER BY created_at, id) AS original_id,
 ROW_NUMBER() OVER (PARTITION BY appointment_id, slot_id ORDER BY updated_at DESC, id DESC) AS latest,
 SUM(version) OVER (PARTITION BY appointment_id, slot_id) AS total_version
FROM schedule_patient_tasks;
UPDATE schedule_patient_tasks tasks JOIN schedule_task_repair repair ON tasks.id = repair.original_id AND repair.latest = 1
SET tasks.reason = repair.reason, tasks.contact_status = repair.contact_status, tasks.note = repair.note,
 tasks.version = repair.total_version, tasks.updated_by = repair.updated_by, tasks.updated_at = repair.updated_at;
DELETE tasks FROM schedule_patient_tasks tasks JOIN schedule_task_repair repair ON tasks.id = repair.id
WHERE repair.id <> repair.original_id;
DROP TEMPORARY TABLE schedule_task_repair;
SET @schedule_key_sql = IF(EXISTS(SELECT 1 FROM information_schema.statistics
 WHERE table_schema = DATABASE() AND table_name = 'schedule_patient_tasks' AND index_name = 'schedule_task_appointment_slot'),
 'SELECT 1', 'ALTER TABLE schedule_patient_tasks ADD UNIQUE KEY schedule_task_appointment_slot (appointment_id, slot_id)');
PREPARE schedule_key_statement FROM @schedule_key_sql;
EXECUTE schedule_key_statement;
DEALLOCATE PREPARE schedule_key_statement;

-- +goose Down
-- Do not mark this data repair rolled back while keeping its merged rows.
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Schedule task repair requires a forward recovery using its archive; automatic rollback is unsupported';
