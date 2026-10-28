-- +goose Up
CREATE TABLE patient_messages (
 id VARCHAR(64) PRIMARY KEY, patient_id VARCHAR(64) NOT NULL, kind VARCHAR(32) NOT NULL,
 title VARCHAR(200) NOT NULL, body TEXT NOT NULL, appointment_id VARCHAR(64) NOT NULL DEFAULT '',
 read_at DATETIME(3) NULL, created_at DATETIME(3) NOT NULL,
 KEY patient_messages_patient_created (patient_id, created_at),
 CONSTRAINT patient_messages_patient_fk FOREIGN KEY (patient_id) REFERENCES patients(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE patient_messages;
