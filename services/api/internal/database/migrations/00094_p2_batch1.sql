-- +goose Up
-- 新增-P2 行 103：方案模板库（共享模板）。个人模板 + 院区共享模板。
ALTER TABLE care_plan_templates
  ADD COLUMN is_shared TINYINT NOT NULL DEFAULT 0,
  ADD COLUMN clinic_id VARCHAR(64) NULL,
  ADD INDEX care_template_shared (is_shared, clinic_id);

-- 新增-P2 行 104：用药/护理提醒打卡。
-- 说明：不建外键（与 00089-00093 一致），应用层保证引用完整；排序规则不 pin，随库默认。
CREATE TABLE care_reminders (
 id VARCHAR(64) PRIMARY KEY,
 patient_id VARCHAR(64) NOT NULL,
 clinic_id VARCHAR(64) NOT NULL,
 kind VARCHAR(20) NOT NULL,
 title VARCHAR(120) NOT NULL,
 detail VARCHAR(1000) NOT NULL DEFAULT '',
 schedule_json JSON NOT NULL,
 start_date DATE NOT NULL,
 end_date DATE NULL,
 created_by VARCHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 active TINYINT NOT NULL DEFAULT 1,
 KEY idx_reminder_patient (patient_id, active),
 KEY idx_reminder_clinic (clinic_id),
 CHECK (kind IN ('medication','care'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE reminder_checkins (
 id VARCHAR(64) PRIMARY KEY,
 reminder_id VARCHAR(64) NOT NULL,
 patient_id VARCHAR(64) NOT NULL,
 checkin_date DATE NOT NULL,
 done TINYINT NOT NULL DEFAULT 1,
 created_at DATETIME(3) NOT NULL,
 UNIQUE KEY uq_checkin (reminder_id, checkin_date),
 KEY idx_checkin_patient (patient_id, checkin_date),
 KEY idx_checkin_reminder (reminder_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 新增-P2 行 105：交接批次（看板按批次聚合）+ 催办通知类型。
ALTER TABLE followup_handoff_receipts
  ADD COLUMN batch_id VARCHAR(64) NULL,
  ADD INDEX handoff_receipt_batch (batch_id);

ALTER TABLE handoff_notifications DROP CONSTRAINT handoff_notifications_chk_1;
ALTER TABLE handoff_notifications ADD CONSTRAINT handoff_notifications_kind_check CHECK (kind IN ('assigned','escalated','nudged'));

-- +goose Down
ALTER TABLE handoff_notifications DROP CONSTRAINT handoff_notifications_kind_check;
ALTER TABLE handoff_notifications ADD CONSTRAINT handoff_notifications_chk_1 CHECK (kind IN ('assigned','escalated'));
ALTER TABLE followup_handoff_receipts DROP INDEX handoff_receipt_batch, DROP COLUMN batch_id;
DROP TABLE reminder_checkins;
DROP TABLE care_reminders;
ALTER TABLE care_plan_templates DROP INDEX care_template_shared, DROP COLUMN clinic_id, DROP COLUMN is_shared;
