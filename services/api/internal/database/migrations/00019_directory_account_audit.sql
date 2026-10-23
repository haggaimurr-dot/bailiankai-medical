-- +goose Up
ALTER TABLE employee_account_events DROP CHECK employee_account_action_allowed;
ALTER TABLE employee_account_events ADD CONSTRAINT employee_account_action_allowed
 CHECK (action IN ('created','activated','deactivated','password-reset','clinics-updated','clinic-created','doctor-linked'));

-- +goose Down
-- Preserve append-only account audit events on rollback.
SELECT 1;
