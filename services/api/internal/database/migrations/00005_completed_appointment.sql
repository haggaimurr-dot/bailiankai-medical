-- +goose Up
ALTER TABLE appointments
 DROP CHECK chk_appointment_status,
 ADD CONSTRAINT chk_appointment_status CHECK (status IN ('pending','confirmed','completed','rejected','cancelled','expired'));
-- +goose Down
-- Atomic ALTER refuses rollback while completed records exist; never rewrites history.
ALTER TABLE appointments
 DROP CHECK chk_appointment_status,
 ADD CONSTRAINT chk_appointment_status CHECK (status IN ('pending','confirmed','rejected','cancelled','expired'));
