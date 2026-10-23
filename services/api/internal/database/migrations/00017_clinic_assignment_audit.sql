-- +goose Up
SET @action_check = (SELECT tc.CONSTRAINT_NAME FROM information_schema.TABLE_CONSTRAINTS tc JOIN information_schema.CHECK_CONSTRAINTS cc ON cc.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA AND cc.CONSTRAINT_NAME = tc.CONSTRAINT_NAME WHERE tc.TABLE_SCHEMA = DATABASE() AND tc.TABLE_NAME = 'employee_account_events' AND cc.CHECK_CLAUSE LIKE '%action%' LIMIT 1);
SET @drop_action_check = CONCAT('ALTER TABLE employee_account_events DROP CHECK `', @action_check, '`');
PREPARE drop_action_check FROM @drop_action_check;
EXECUTE drop_action_check;
DEALLOCATE PREPARE drop_action_check;
ALTER TABLE employee_account_events ADD CONSTRAINT employee_account_action_allowed CHECK (action IN ('created','activated','deactivated','password-reset','clinics-updated'));

-- +goose Down
SELECT 1;
