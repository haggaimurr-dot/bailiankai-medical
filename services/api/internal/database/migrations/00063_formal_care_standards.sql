-- +goose Up
ALTER TABLE followup_templates
 ADD COLUMN standard_status VARCHAR(16) NOT NULL DEFAULT 'draft',
 ADD COLUMN applicable_treatment VARCHAR(200) NOT NULL DEFAULT '',
 ADD COLUMN nursing_instructions VARCHAR(2000) NOT NULL DEFAULT '',
 ADD COLUMN followup_checklist VARCHAR(2000) NOT NULL DEFAULT '';
-- +goose Down
ALTER TABLE followup_templates
 DROP COLUMN followup_checklist, DROP COLUMN nursing_instructions,
 DROP COLUMN applicable_treatment, DROP COLUMN standard_status;
