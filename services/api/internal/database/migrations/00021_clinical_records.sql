-- +goose Up
ALTER TABLE consultations
 ADD COLUMN visit_type VARCHAR(16) NOT NULL DEFAULT 'initial',
 ADD COLUMN parent_consultation_id VARCHAR(64) NULL,
 ADD COLUMN appointment_id VARCHAR(64) NULL,
 ADD COLUMN visited_at DATETIME(3) NULL,
 ADD COLUMN next_visit_at DATETIME(3) NULL,
 ADD COLUMN stage_index INT NOT NULL DEFAULT 0,
 ADD COLUMN stage_status VARCHAR(16) NOT NULL DEFAULT 'not_started',
 ADD CONSTRAINT fk_consultation_parent FOREIGN KEY (parent_consultation_id) REFERENCES consultations(id),
 ADD CONSTRAINT fk_consultation_appointment FOREIGN KEY (appointment_id) REFERENCES appointments(id);
UPDATE consultations SET visited_at = recorded_at;
ALTER TABLE consultations MODIFY visited_at DATETIME(3) NOT NULL, ADD INDEX consultations_patient_visit (patient_id, visited_at);
ALTER TABLE treatments
 ADD COLUMN stage_index INT NOT NULL DEFAULT 0,
 ADD COLUMN `procedure` VARCHAR(100) NOT NULL DEFAULT '',
 ADD COLUMN body_area VARCHAR(100) NOT NULL DEFAULT '',
 ADD COLUMN device VARCHAR(100) NOT NULL DEFAULT '',
 ADD COLUMN parameters VARCHAR(2000) NOT NULL DEFAULT '';
ALTER TABLE treatment_revisions
 ADD COLUMN stage_index INT NOT NULL DEFAULT 0,
 ADD COLUMN `procedure` VARCHAR(100) NOT NULL DEFAULT '',
 ADD COLUMN body_area VARCHAR(100) NOT NULL DEFAULT '',
 ADD COLUMN device VARCHAR(100) NOT NULL DEFAULT '',
 ADD COLUMN parameters VARCHAR(2000) NOT NULL DEFAULT '';

CREATE INDEX followup_patient_submitted ON followup_tasks(patient_id, submitted_at);
CREATE INDEX followup_patient_reviewed ON followup_tasks(patient_id, reviewed_at);

-- +goose Down
DROP INDEX followup_patient_reviewed ON followup_tasks;
DROP INDEX followup_patient_submitted ON followup_tasks;
ALTER TABLE treatment_revisions DROP COLUMN stage_index, DROP COLUMN `procedure`, DROP COLUMN body_area, DROP COLUMN device, DROP COLUMN parameters;
ALTER TABLE treatments DROP COLUMN stage_index, DROP COLUMN `procedure`, DROP COLUMN body_area, DROP COLUMN device, DROP COLUMN parameters;
ALTER TABLE consultations DROP FOREIGN KEY fk_consultation_parent, DROP FOREIGN KEY fk_consultation_appointment;
ALTER TABLE consultations DROP INDEX consultations_patient_visit, DROP COLUMN visit_type, DROP COLUMN parent_consultation_id, DROP COLUMN appointment_id, DROP COLUMN visited_at, DROP COLUMN next_visit_at, DROP COLUMN stage_index, DROP COLUMN stage_status;
