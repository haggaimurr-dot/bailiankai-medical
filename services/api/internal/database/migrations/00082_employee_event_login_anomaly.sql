-- NOTE: DROP CONSTRAINT (not DROP CHECK) is the portable form: MariaDB
-- rejects DROP CHECK, while MySQL 8.0.19+ accepts DROP CONSTRAINT for any
-- constraint type. The older 00017/00019/00050 migrations use DROP CHECK
-- (MySQL-only); do not copy that form for new migrations.
-- +goose Up
ALTER TABLE employee_account_events DROP CONSTRAINT employee_account_action_allowed;
ALTER TABLE employee_account_events ADD CONSTRAINT employee_account_action_allowed
 CHECK (action IN ('created','activated','deactivated','password-reset','clinics-updated','clinic-created','doctor-linked','password-changed','access-updated','login-anomaly'));

-- +goose Down
ALTER TABLE employee_account_events DROP CONSTRAINT employee_account_action_allowed;
ALTER TABLE employee_account_events ADD CONSTRAINT employee_account_action_allowed
 CHECK (action IN ('created','activated','deactivated','password-reset','clinics-updated','clinic-created','doctor-linked','password-changed','access-updated'));
