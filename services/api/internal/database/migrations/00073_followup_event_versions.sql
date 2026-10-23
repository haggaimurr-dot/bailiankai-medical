-- +goose Up
-- Zero denotes legacy events whose exact resource version was not recorded.
ALTER TABLE followup_assignment_events ADD COLUMN task_version INT NOT NULL DEFAULT 0;
ALTER TABLE followup_staff_events ADD COLUMN staff_version INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE followup_staff_events DROP COLUMN staff_version;
ALTER TABLE followup_assignment_events DROP COLUMN task_version;
