-- +goose Up
-- NOTE: do NOT pin COLLATE here. These tables have FKs to patients/clinics.
-- The two environments disagree on that collation (CI MySQL 8.4 fresh DB uses
-- utf8mb4_0900_ai_ci per migrations 00001/00002; Shanghai MariaDB uses
-- utf8mb4_unicode_ci as its DB default). Pinning either one breaks the other
-- with MySQL error 3780 (FK column incompatibility). Leaving COLLATE out lets
-- each DB fall back to its own default, which matches its patients/clinics.
CREATE TABLE patient_messages (
 id VARCHAR(64) PRIMARY KEY, patient_id VARCHAR(64) NOT NULL, kind VARCHAR(32) NOT NULL,
 title VARCHAR(200) NOT NULL, body TEXT NOT NULL, appointment_id VARCHAR(64) NOT NULL DEFAULT '',
 read_at DATETIME(3) NULL, created_at DATETIME(3) NOT NULL,
 KEY patient_messages_patient_created (patient_id, created_at),
 CONSTRAINT patient_messages_patient_fk FOREIGN KEY (patient_id) REFERENCES patients(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE patient_messages;
