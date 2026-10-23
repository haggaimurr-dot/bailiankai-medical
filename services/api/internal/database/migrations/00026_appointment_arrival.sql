-- +goose Up
-- Historical confirmation/treatment is never backfilled as an arrival.
ALTER TABLE appointments
 ADD COLUMN arrived_at DATETIME(3) NULL,
 ADD COLUMN arrived_by VARCHAR(64) NOT NULL DEFAULT '',
 ADD CONSTRAINT chk_appointment_arrival CHECK (
   (arrived_at IS NULL AND arrived_by = '') OR
   (arrived_at IS NOT NULL AND arrived_by <> '' AND status IN ('confirmed', 'completed'))
 );

-- +goose Down
ALTER TABLE appointments
 DROP CHECK chk_appointment_arrival,
 DROP COLUMN arrived_by,
 DROP COLUMN arrived_at;
