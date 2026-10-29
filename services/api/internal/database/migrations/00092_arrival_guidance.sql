-- +goose Up
-- 新增-P2 行 98：到诊后自动推送面诊流程指引。按院区配置指引文案，到诊登记后自动发患者站内消息。
CREATE TABLE arrival_guidance_settings (
    clinic_id VARCHAR(64) PRIMARY KEY,
    content TEXT NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    updated_at DATETIME(3) NOT NULL,
    updated_by VARCHAR(64) NOT NULL DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
