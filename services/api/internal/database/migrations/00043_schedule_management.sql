-- +goose Up
CREATE TABLE schedule_settings (
 clinic_id VARCHAR(64) PRIMARY KEY,
 templates JSON NOT NULL,
 holidays JSON NOT NULL,
 version INT NOT NULL,
 updated_by VARCHAR(64) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 CONSTRAINT fk_schedule_settings_clinic FOREIGN KEY (clinic_id) REFERENCES clinics(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE schedule_patient_tasks (
 id VARCHAR(64) PRIMARY KEY,
 appointment_id VARCHAR(64) NOT NULL,
 slot_id VARCHAR(64) NOT NULL,
 reason VARCHAR(200) NOT NULL,
 contact_status VARCHAR(32) NOT NULL DEFAULT 'todo',
 note VARCHAR(500) NOT NULL DEFAULT '',
 version INT NOT NULL DEFAULT 1,
 updated_by VARCHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 KEY schedule_tasks_slot (slot_id, created_at),
 UNIQUE KEY schedule_task_appointment_slot (appointment_id, slot_id),
 CONSTRAINT fk_schedule_task_appointment FOREIGN KEY (appointment_id) REFERENCES appointments(id) ON DELETE CASCADE,
 CONSTRAINT fk_schedule_task_slot FOREIGN KEY (slot_id) REFERENCES slots(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
INSERT INTO schedule_patient_tasks (id, appointment_id, slot_id, reason, contact_status, note, version, updated_by, created_at, updated_at)
SELECT CONCAT('schedule-task-', REPLACE(UUID(), '-', '')), a.id, a.slot_id,
 COALESCE((SELECT e.reason FROM schedule_events e WHERE e.slot_id = a.slot_id AND e.action = 'suspended' ORDER BY e.created_at DESC, e.id DESC LIMIT 1), '排班关闭'),
 'todo', '', 1, 'migration', NOW(3), NOW(3)
FROM appointments a JOIN slots s ON s.id = a.slot_id
WHERE s.status = 'closed' AND s.starts_at > NOW(3) AND a.status IN ('pending', 'confirmed', 'waiting');
-- +goose Down
DROP TABLE schedule_patient_tasks;
DROP TABLE schedule_settings;
