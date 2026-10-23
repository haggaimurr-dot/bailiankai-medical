-- +goose Up
ALTER TABLE employee_accounts ADD COLUMN clinic_ids JSON NULL;
UPDATE employee_accounts SET clinic_ids = COALESCE((SELECT JSON_ARRAYAGG(clinics.id) FROM clinics), JSON_ARRAY()) WHERE role = 'manager';
UPDATE employee_accounts account JOIN doctors ON doctors.id = account.staff_id SET account.clinic_ids = JSON_ARRAY(doctors.clinic_id) WHERE account.role = 'clinician';
UPDATE employee_accounts SET clinic_ids = JSON_ARRAY() WHERE clinic_ids IS NULL;
ALTER TABLE employee_accounts MODIFY clinic_ids JSON NOT NULL;
SET @role_check = (SELECT tc.CONSTRAINT_NAME FROM information_schema.TABLE_CONSTRAINTS tc JOIN information_schema.CHECK_CONSTRAINTS cc ON cc.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA AND cc.CONSTRAINT_NAME = tc.CONSTRAINT_NAME WHERE tc.TABLE_SCHEMA = DATABASE() AND tc.TABLE_NAME = 'employee_accounts' AND cc.CHECK_CLAUSE LIKE '%role%' LIMIT 1);
SET @drop_role_check = CONCAT('ALTER TABLE employee_accounts DROP CHECK `', @role_check, '`');
PREPARE drop_role_check FROM @drop_role_check;
EXECUTE drop_role_check;
DEALLOCATE PREPARE drop_role_check;
ALTER TABLE employee_accounts ADD CONSTRAINT employee_role_allowed CHECK (role IN ('manager','clinician','doctor','nurse','customer_service','operations'));
ALTER TABLE consultations ADD COLUMN clinic_id VARCHAR(64) NULL;
UPDATE consultations JOIN (SELECT appointments.patient_id, MIN(slots.clinic_id) AS clinic_id FROM appointments JOIN slots ON slots.id = appointments.slot_id GROUP BY appointments.patient_id HAVING COUNT(DISTINCT slots.clinic_id) = 1) ownership ON ownership.patient_id = consultations.patient_id SET consultations.clinic_id = ownership.clinic_id;
ALTER TABLE consultations ADD CONSTRAINT fk_consultations_clinic FOREIGN KEY (clinic_id) REFERENCES clinics(id);
CREATE TABLE privacy_consent_events (
 id VARCHAR(64) PRIMARY KEY,
 patient_id VARCHAR(64) NOT NULL,
 policy_version VARCHAR(64) NOT NULL,
 policy_content TEXT NOT NULL,
 action VARCHAR(16) NOT NULL,
 source VARCHAR(24) NOT NULL,
 recorded_at DATETIME(3) NOT NULL,
 KEY privacy_patient_timeline (patient_id, recorded_at, id),
 FOREIGN KEY (patient_id) REFERENCES patients(id),
 CHECK (action IN ('accepted','revoked'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE privacy_consent_events;
ALTER TABLE consultations DROP FOREIGN KEY fk_consultations_clinic;
ALTER TABLE consultations DROP COLUMN clinic_id;
UPDATE employee_accounts SET active = FALSE, role = 'clinician' WHERE role IN ('doctor','nurse','customer_service','operations');
ALTER TABLE employee_accounts DROP CHECK employee_role_allowed;
ALTER TABLE employee_accounts ADD CONSTRAINT employee_role_allowed CHECK (role IN ('manager','clinician'));
ALTER TABLE employee_accounts DROP COLUMN clinic_ids;
