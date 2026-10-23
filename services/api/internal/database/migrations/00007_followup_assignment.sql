-- +goose Up
CREATE TABLE followup_staff (
 id VARCHAR(64) PRIMARY KEY,
 display_name VARCHAR(80) NOT NULL,
 role VARCHAR(24) NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'on_duty',
 version INT NOT NULL DEFAULT 1,
 last_handoff_to VARCHAR(64) NULL,
 last_leave_reason VARCHAR(300) NOT NULL DEFAULT '',
 CHECK (status IN ('on_duty','off_duty')),
 CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO followup_staff (id, display_name, role, status, version)
SELECT id, name, 'doctor', 'on_duty', 1 FROM doctors;
INSERT INTO followup_staff (id, display_name, role, status, version)
VALUES ('nurse-demo-1', '演示随访护士', 'nurse', 'on_duty', 1)
ON DUPLICATE KEY UPDATE id = id;

ALTER TABLE followup_tasks
 ADD COLUMN assignee_id VARCHAR(64) NULL,
 ADD COLUMN assigned_at DATETIME(3) NULL;
UPDATE followup_tasks
 JOIN appointments ON appointments.id = followup_tasks.appointment_id
 JOIN slots ON slots.id = appointments.slot_id
 JOIN treatments ON treatments.id = followup_tasks.treatment_id
 SET followup_tasks.assignee_id = slots.doctor_id,
     followup_tasks.assigned_at = treatments.recorded_at;
ALTER TABLE followup_tasks
 MODIFY assignee_id VARCHAR(64) NOT NULL,
 MODIFY assigned_at DATETIME(3) NOT NULL,
 ADD KEY followup_assignee_queue (assignee_id, status, due_at),
 ADD CONSTRAINT fk_followup_assignee FOREIGN KEY (assignee_id) REFERENCES followup_staff(id);

CREATE TABLE followup_assignment_events (
 id VARCHAR(64) PRIMARY KEY,
 task_id VARCHAR(64) NOT NULL,
 from_staff_id VARCHAR(64) NULL,
 to_staff_id VARCHAR(64) NOT NULL,
 event_type VARCHAR(16) NOT NULL,
 reason VARCHAR(300) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 KEY followup_assignment_history (task_id, created_at, id),
 FOREIGN KEY (task_id) REFERENCES followup_tasks(id),
 FOREIGN KEY (from_staff_id) REFERENCES followup_staff(id),
 FOREIGN KEY (to_staff_id) REFERENCES followup_staff(id),
 CHECK (event_type IN ('initial','assign','leave'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE followup_staff_events (
 id VARCHAR(64) PRIMARY KEY,
 staff_id VARCHAR(64) NOT NULL,
 event_type VARCHAR(16) NOT NULL,
 to_staff_id VARCHAR(64) NULL,
 reason VARCHAR(300) NOT NULL,
 transferred_count INT NOT NULL DEFAULT 0,
 actor_id VARCHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 KEY followup_staff_history (staff_id, created_at, id),
 FOREIGN KEY (staff_id) REFERENCES followup_staff(id),
 FOREIGN KEY (to_staff_id) REFERENCES followup_staff(id),
 CHECK (event_type IN ('leave','return')),
 CHECK (transferred_count >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO followup_assignment_events
 (id, task_id, from_staff_id, to_staff_id, event_type, reason, actor_id, created_at)
SELECT UUID(), id, NULL, assignee_id, 'initial', '既有任务负责人回填', 'system', assigned_at
FROM followup_tasks;

-- +goose Down
DROP TABLE followup_assignment_events;
DROP TABLE followup_staff_events;
ALTER TABLE followup_tasks DROP FOREIGN KEY fk_followup_assignee;
ALTER TABLE followup_tasks DROP KEY followup_assignee_queue;
ALTER TABLE followup_tasks DROP COLUMN assigned_at, DROP COLUMN assignee_id;
DROP TABLE followup_staff;
