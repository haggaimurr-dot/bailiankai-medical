-- +goose Up
ALTER TABLE employee_account_events ADD COLUMN details TEXT NULL;

-- +goose Down
ALTER TABLE employee_account_events DROP COLUMN details;
