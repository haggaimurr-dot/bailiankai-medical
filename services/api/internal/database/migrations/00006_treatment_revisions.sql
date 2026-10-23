-- +goose Up
ALTER TABLE treatments
 ADD COLUMN version INT NOT NULL DEFAULT 1,
 ADD COLUMN corrected_at DATETIME(3) NULL,
 ADD CONSTRAINT chk_treatment_version CHECK (version > 0);

CREATE TABLE treatment_revisions (
 id VARCHAR(64) PRIMARY KEY,
 treatment_id VARCHAR(64) NOT NULL,
 version INT NOT NULL,
 plan_version_id VARCHAR(64) NOT NULL,
 summary VARCHAR(1000) NOT NULL,
 notes VARCHAR(2000) NOT NULL,
 treated_at DATETIME(3) NOT NULL,
 changed_at DATETIME(3) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 correction_reason VARCHAR(300) NOT NULL,
 UNIQUE KEY treatment_revision_number (treatment_id, version),
 FOREIGN KEY (treatment_id) REFERENCES treatments(id),
 FOREIGN KEY (plan_version_id) REFERENCES care_plan_versions(id),
 CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO treatment_revisions
 (id, treatment_id, version, plan_version_id, summary, notes, treated_at, changed_at, actor_id, correction_reason)
SELECT UUID(), id, 1, plan_version_id, summary, notes, treated_at, recorded_at, 'demo-clinician', ''
FROM treatments;

-- +goose Down
DROP TABLE treatment_revisions;
ALTER TABLE treatments DROP CHECK chk_treatment_version, DROP COLUMN corrected_at, DROP COLUMN version;
