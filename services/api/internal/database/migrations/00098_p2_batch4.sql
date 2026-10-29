-- +goose Up
-- P2 batch 4 (rows 112/114/115). Row 112 (PDF) is frontend print view, no migration.
-- 行114: 提醒偏好——各类提醒开关与提前量。
-- 行115: 双因素验证——员工 TOTP 密钥。
-- NOTE: no COLLATE pin, no FKs (per the 00089-00093 convention).
CREATE TABLE notification_preferences (
 patient_id VARCHAR(64) PRIMARY KEY,
 appointment_reminder BOOLEAN NOT NULL DEFAULT TRUE,
 followup_reminder BOOLEAN NOT NULL DEFAULT TRUE,
 care_reminder BOOLEAN NOT NULL DEFAULT TRUE,
 lead_hours INT NOT NULL DEFAULT 24,
 updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE employee_2fa (
 employee_id VARCHAR(64) PRIMARY KEY,
 secret VARCHAR(64) NOT NULL,
 enabled BOOLEAN NOT NULL DEFAULT FALSE,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS employee_2fa;
DROP TABLE IF EXISTS notification_preferences;
