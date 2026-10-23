-- +goose Up
CREATE TABLE patient_charts (
 patient_id VARCHAR(64) PRIMARY KEY,
 clinic_id VARCHAR(64) NOT NULL,
 staff_id VARCHAR(64) NOT NULL DEFAULT '',
 gender VARCHAR(16) NOT NULL DEFAULT 'unknown',
 birth_date VARCHAR(10) NOT NULL DEFAULT '',
 contact_phone VARCHAR(32) NOT NULL DEFAULT '',
 tags JSON NOT NULL,
 version INT NOT NULL DEFAULT 1,
 KEY chart_scope (clinic_id, staff_id),
 CONSTRAINT fk_chart_patient FOREIGN KEY (patient_id) REFERENCES patients(id),
 CONSTRAINT fk_chart_clinic FOREIGN KEY (clinic_id) REFERENCES clinics(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
ALTER TABLE consultations
 ADD medical_history TEXT NULL,
 ADD allergy_history TEXT NULL,
 ADD treatment_history TEXT NULL,
 ADD skin_type VARCHAR(32) NOT NULL DEFAULT '',
 ADD goals TEXT NULL;
CREATE TABLE consultation_templates (
 id VARCHAR(64) PRIMARY KEY,
 clinic_id VARCHAR(64) NOT NULL,
 recorded_by VARCHAR(64) NOT NULL,
 name VARCHAR(64) NOT NULL,
 visit_type VARCHAR(16) NOT NULL,
 chief_complaint VARCHAR(1000) NOT NULL,
 history TEXT NOT NULL,
 assessment TEXT NOT NULL,
 advice TEXT NOT NULL,
 medical_history TEXT NOT NULL,
 allergy_history TEXT NOT NULL,
 treatment_history TEXT NOT NULL,
 skin_type VARCHAR(32) NOT NULL,
 goals TEXT NOT NULL,
 created_at DATETIME(3) NOT NULL,
 KEY template_scope (clinic_id, recorded_by),
 CONSTRAINT fk_template_clinic FOREIGN KEY (clinic_id) REFERENCES clinics(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
-- +goose Down
DROP TABLE consultation_templates;
ALTER TABLE consultations DROP medical_history, DROP allergy_history, DROP treatment_history, DROP skin_type, DROP goals;
DROP TABLE patient_charts;
