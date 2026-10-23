-- +goose Up
CREATE TABLE case_records (
 id VARCHAR(64) PRIMARY KEY,
 appointment_id VARCHAR(64) NOT NULL,
 patient_id VARCHAR(64) NOT NULL,
 clinic_id VARCHAR(64) NOT NULL,
 content_json JSON NOT NULL,
 version INT NOT NULL,
 revision INT NOT NULL,
 consent_revision INT NOT NULL DEFAULT 0,
 review_status VARCHAR(20) NOT NULL DEFAULT 'pending',
 review_note VARCHAR(1000) NOT NULL DEFAULT '',
 reviewer_id VARCHAR(64) NOT NULL DEFAULT '',
 reviewer_name VARCHAR(120) NOT NULL DEFAULT '',
 reviewed_at DATETIME(3) NULL,
 published_at DATETIME(3) NULL,
 FOREIGN KEY (appointment_id) REFERENCES appointments(id),
 FOREIGN KEY (patient_id) REFERENCES patients(id),
 FOREIGN KEY (clinic_id) REFERENCES clinics(id),
 INDEX case_clinic (clinic_id, published_at, id),
 INDEX case_patient (patient_id, id)
);
CREATE TABLE case_images (
 id VARCHAR(64) PRIMARY KEY,
 case_id VARCHAR(64) NOT NULL,
 sha256 VARCHAR(64) NOT NULL,
 size_bytes BIGINT NOT NULL,
 FOREIGN KEY (case_id) REFERENCES case_records(id)
);
CREATE TABLE case_events (
 id VARCHAR(64) PRIMARY KEY,
 case_id VARCHAR(64) NOT NULL,
 version INT NOT NULL,
 action VARCHAR(20) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 snapshot_json JSON NOT NULL,
 policy_version VARCHAR(64) NOT NULL DEFAULT '',
 policy_content TEXT NOT NULL,
 created_at DATETIME(3) NOT NULL,
 FOREIGN KEY (case_id) REFERENCES case_records(id),
 UNIQUE KEY case_event_version (case_id, version)
);
-- +goose Down
DROP TABLE case_events;
DROP TABLE case_images;
DROP TABLE case_records;
