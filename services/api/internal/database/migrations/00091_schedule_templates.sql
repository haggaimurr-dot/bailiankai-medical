-- +goose Up
-- 排班模板与节假日：模板存每周排班模式 JSON，一键套用到日期范围；节假日日期列表，生成排班时自动跳过。不要在这里 pin COLLATE。
CREATE TABLE schedule_templates (
 id VARCHAR(64) PRIMARY KEY,
 clinic_id VARCHAR(64) NOT NULL,
 name VARCHAR(100) NOT NULL,
 pattern_json LONGTEXT NOT NULL,
 created_by VARCHAR(64) NOT NULL DEFAULT '',
 created_at DATETIME(6) NOT NULL,
 INDEX idx_schedule_templates_clinic (clinic_id)
);
CREATE TABLE holidays (
 holiday_date DATE PRIMARY KEY,
 name VARCHAR(100) NOT NULL,
 created_at DATETIME(6) NOT NULL
);

-- +goose Down
DROP TABLE holidays;
DROP TABLE schedule_templates;
