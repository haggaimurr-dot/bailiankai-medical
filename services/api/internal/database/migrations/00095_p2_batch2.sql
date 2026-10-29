-- +goose Up
-- P2 batch 2 (rows 106/107/108): recovery stage plans + milestones (row 107).
-- NOTE: do NOT pin COLLATE here. These tables have FKs to patients; the two
-- environments disagree on collations and MariaDB 10.11 inherits
-- utf8mb4_general_ci for new tables (vs utf8mb4_unicode_ci on existing ones),
-- so new tables carry NO foreign keys (application-layer integrity, per the
-- 00089-00093 convention) and no COLLATE pin.
CREATE TABLE recovery_stage_plans (
 id VARCHAR(64) PRIMARY KEY,
 patient_id VARCHAR(64) NOT NULL,
 appointment_id VARCHAR(64) NOT NULL DEFAULT '',
 clinic_id VARCHAR(64) NOT NULL,
 start_date DATE NOT NULL,
 status VARCHAR(20) NOT NULL DEFAULT 'active',
 created_by VARCHAR(64) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL,
 KEY recovery_stage_plans_patient (patient_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE recovery_stage_milestones (
 id VARCHAR(64) PRIMARY KEY,
 plan_id VARCHAR(64) NOT NULL,
 day_offset INT NOT NULL,
 stage_label VARCHAR(64) NOT NULL,
 summary TEXT NOT NULL DEFAULT '',
 notified BOOLEAN NOT NULL DEFAULT FALSE,
 notified_at DATETIME(3) NULL,
 created_at DATETIME(3) NOT NULL,
 UNIQUE KEY recovery_stage_milestone_plan_day (plan_id, day_offset),
 KEY recovery_stage_milestones_plan (plan_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS recovery_stage_milestones;
DROP TABLE IF EXISTS recovery_stage_plans;

