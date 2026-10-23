-- +goose Up
CREATE TABLE treatments (
 id VARCHAR(64) PRIMARY KEY,
 appointment_id VARCHAR(64) NOT NULL UNIQUE,
 patient_id VARCHAR(64) NOT NULL,
 plan_version_id VARCHAR(64) NOT NULL,
 summary VARCHAR(1000) NOT NULL,
 notes VARCHAR(2000) NOT NULL,
 treated_at DATETIME(3) NOT NULL,
 recorded_at DATETIME(3) NOT NULL,
 request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 FOREIGN KEY (appointment_id) REFERENCES appointments(id),
 FOREIGN KEY (patient_id) REFERENCES patients(id),
 FOREIGN KEY (plan_version_id) REFERENCES care_plan_versions(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE followup_tasks (
 id VARCHAR(64) PRIMARY KEY,
 appointment_id VARCHAR(64) NOT NULL,
 treatment_id VARCHAR(64) NOT NULL,
 patient_id VARCHAR(64) NOT NULL,
 title VARCHAR(100) NOT NULL,
 due_at DATETIME(3) NOT NULL,
 status VARCHAR(16) NOT NULL,
 version INT NOT NULL,
 symptoms VARCHAR(2000) NOT NULL DEFAULT '',
 severity INT NOT NULL DEFAULT 0,
 concern BOOLEAN NOT NULL DEFAULT FALSE,
 response VARCHAR(2000) NOT NULL DEFAULT '',
 disposition VARCHAR(1000) NOT NULL DEFAULT '',
 submitted_at DATETIME(3) NULL,
 reviewed_at DATETIME(3) NULL,
 UNIQUE KEY followup_treatment_due (treatment_id, due_at),
 KEY followup_queue (status, due_at, id),
 KEY followup_patient (patient_id, due_at),
 FOREIGN KEY (appointment_id) REFERENCES appointments(id),
 FOREIGN KEY (treatment_id) REFERENCES treatments(id),
 FOREIGN KEY (patient_id) REFERENCES patients(id),
 CHECK (status IN ('pending','submitted','alert','reviewed','handled')),
 CHECK (severity BETWEEN 0 AND 3), CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE followup_photos (
 id VARCHAR(64) PRIMARY KEY,
 task_id VARCHAR(64) NOT NULL,
 appointment_id VARCHAR(64) NOT NULL,
 patient_id VARCHAR(64) NOT NULL,
 angle VARCHAR(16) NOT NULL,
 size_bytes BIGINT NOT NULL,
 sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 uploaded_at DATETIME(3) NOT NULL,
 UNIQUE KEY followup_photo_angle (task_id, angle),
 FOREIGN KEY (task_id) REFERENCES followup_tasks(id),
 FOREIGN KEY (appointment_id) REFERENCES appointments(id),
 FOREIGN KEY (patient_id) REFERENCES patients(id),
 CHECK (angle IN ('front','left','right')),
 CHECK (size_bytes BETWEEN 1 AND 8388608)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE followup_events (
 id VARCHAR(64) PRIMARY KEY,
 appointment_id VARCHAR(64) NOT NULL,
 task_id VARCHAR(64) NOT NULL,
 actor_type VARCHAR(16) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 action VARCHAR(32) NOT NULL,
 detail_json JSON NOT NULL,
 created_at DATETIME(3) NOT NULL,
 KEY followup_audit (appointment_id, created_at),
 FOREIGN KEY (appointment_id) REFERENCES appointments(id),
 CHECK (actor_type IN ('patient','clinician'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
-- +goose Down
DROP TABLE followup_events;
DROP TABLE followup_photos;
DROP TABLE followup_tasks;
DROP TABLE treatments;
