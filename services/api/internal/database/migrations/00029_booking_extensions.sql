-- +goose Up
ALTER TABLE appointments
 DROP CHECK chk_appointment_status,
 DROP INDEX appointments_active_patient_slot,
 DROP COLUMN active_slot_id,
 ADD COLUMN active_slot_id VARCHAR(64) GENERATED ALWAYS AS (CASE WHEN status IN ('waiting','pending','confirmed') THEN slot_id ELSE NULL END) STORED,
 ADD UNIQUE KEY appointments_active_patient_slot (patient_id, active_slot_id),
 ADD CONSTRAINT chk_appointment_status CHECK (status IN ('waiting','pending','confirmed','completed','rejected','cancelled','expired'));
ALTER TABLE appointment_events
 ADD COLUMN sequence BIGINT UNSIGNED NOT NULL AUTO_INCREMENT UNIQUE,
 ADD COLUMN request_key VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NULL,
 ADD COLUMN request_hash VARCHAR(64) NOT NULL DEFAULT '',
 ADD COLUMN detail VARCHAR(500) NOT NULL DEFAULT '',
 ADD UNIQUE KEY appointment_events_request (appointment_id, request_key);

-- +goose Down
ALTER TABLE appointments
 DROP CHECK chk_appointment_status,
 ADD CONSTRAINT chk_appointment_status CHECK (status IN ('pending','confirmed','completed','rejected','cancelled','expired')),
 DROP INDEX appointments_active_patient_slot,
 DROP COLUMN active_slot_id,
 ADD COLUMN active_slot_id VARCHAR(64) GENERATED ALWAYS AS (CASE WHEN status IN ('pending','confirmed') THEN slot_id ELSE NULL END) STORED,
 ADD UNIQUE KEY appointments_active_patient_slot (patient_id, active_slot_id);
ALTER TABLE appointment_events DROP INDEX appointment_events_request, DROP COLUMN request_key, DROP COLUMN request_hash, DROP COLUMN detail, DROP COLUMN sequence;
