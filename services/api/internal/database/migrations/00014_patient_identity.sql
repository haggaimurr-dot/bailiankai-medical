-- +goose Up
ALTER TABLE patients
    ADD wechat_app_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    ADD wechat_open_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NULL,
    ADD phone VARCHAR(11) NOT NULL DEFAULT '',
    ADD phone_authorization VARCHAR(16) NOT NULL DEFAULT 'not_granted',
    ADD phone_authorized_at DATETIME(3) NULL,
    ADD version INT NOT NULL DEFAULT 1,
    ADD UNIQUE KEY patients_wechat_identity (wechat_app_id, wechat_open_id),
    ADD CONSTRAINT chk_patient_phone_authorization CHECK (phone_authorization IN ('not_granted', 'granted', 'revoked')),
    ADD CONSTRAINT chk_patient_version CHECK (version > 0);

-- +goose Down
ALTER TABLE patients
    DROP CHECK chk_patient_version,
    DROP CHECK chk_patient_phone_authorization,
    DROP INDEX patients_wechat_identity,
    DROP COLUMN version,
    DROP COLUMN phone_authorized_at,
    DROP COLUMN phone_authorization,
    DROP COLUMN phone,
    DROP COLUMN wechat_open_id,
    DROP COLUMN wechat_app_id;
