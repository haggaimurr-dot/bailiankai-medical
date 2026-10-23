-- +goose Up
CREATE TABLE followup_handoff_receipts (
 id VARCHAR(64) PRIMARY KEY,
 task_id VARCHAR(64) NOT NULL,
 from_staff_id VARCHAR(64) NOT NULL,
 to_staff_id VARCHAR(64) NOT NULL,
 reason VARCHAR(300) NOT NULL,
 assigned_by VARCHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'pending',
 confirmed_by VARCHAR(64) NULL,
 confirmed_at DATETIME(3) NULL,
 superseded_at DATETIME(3) NULL,
 version INT NOT NULL DEFAULT 1,
 KEY handoff_receipt_inbox (to_staff_id, status, created_at, id),
 KEY handoff_receipt_task (task_id, status),
 FOREIGN KEY (task_id) REFERENCES followup_tasks(id),
 FOREIGN KEY (from_staff_id) REFERENCES followup_staff(id),
 FOREIGN KEY (to_staff_id) REFERENCES followup_staff(id),
 CHECK (status IN ('pending','confirmed','superseded')),
 CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE followup_handoff_receipts;
