-- +goose Up
-- P2 batch 5 (rows 117-120, 123).
-- 行117: 模板效果统计——消息点击回统计。
-- 行118: 接口调用监控与慢请求告警。
-- 行119: 关键表数据变更日报。
-- 行123: 异常访问行为信号与告警。
-- NOTE: no COLLATE pin, no FKs (per the 00089-00093 convention).

CREATE TABLE message_clicks (
 id VARCHAR(64) PRIMARY KEY,
 template_id VARCHAR(64) NOT NULL,
 notification_id VARCHAR(64) NOT NULL,
 patient_id VARCHAR(64) NOT NULL,
 clicked_at DATETIME(3) NOT NULL,
 KEY idx_clicks_template (template_id),
 KEY idx_clicks_notification (notification_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE api_request_stats (
 route VARCHAR(160) NOT NULL,
 method VARCHAR(16) NOT NULL,
 stat_date DATE NOT NULL,
 total BIGINT NOT NULL DEFAULT 0,
 err_4xx BIGINT NOT NULL DEFAULT 0,
 err_5xx BIGINT NOT NULL DEFAULT 0,
 sum_ms BIGINT NOT NULL DEFAULT 0,
 max_ms INT NOT NULL DEFAULT 0,
 PRIMARY KEY (route, method, stat_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE api_slow_alerts (
 id VARCHAR(64) PRIMARY KEY,
 route VARCHAR(160) NOT NULL,
 method VARCHAR(16) NOT NULL,
 status INT NOT NULL,
 duration_ms INT NOT NULL,
 threshold_ms INT NOT NULL,
 created_at DATETIME(3) NOT NULL,
 KEY idx_slow_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE monitoring_settings (
 k VARCHAR(64) PRIMARY KEY,
 v VARCHAR(255) NOT NULL,
 updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE data_daily_reports (
 report_date DATE NOT NULL,
 table_name VARCHAR(64) NOT NULL,
 row_count BIGINT NOT NULL DEFAULT 0,
 created_at DATETIME(3) NOT NULL,
 PRIMARY KEY (report_date, table_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE security_signals (
 id VARCHAR(64) PRIMARY KEY,
 actor_type VARCHAR(16) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 kind VARCHAR(32) NOT NULL,
 resource_type VARCHAR(32) NOT NULL,
 resource_id VARCHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 KEY idx_signals_actor (actor_type, actor_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE security_alerts (
 id VARCHAR(64) PRIMARY KEY,
 actor_type VARCHAR(16) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 kind VARCHAR(32) NOT NULL,
 detail VARCHAR(500) NOT NULL,
 alerted_at DATETIME(3) NOT NULL,
 read_at DATETIME(3) NULL,
 KEY idx_alerts_created (alerted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS security_alerts;
DROP TABLE IF EXISTS security_signals;
DROP TABLE IF EXISTS data_daily_reports;
DROP TABLE IF EXISTS monitoring_settings;
DROP TABLE IF EXISTS api_slow_alerts;
DROP TABLE IF EXISTS api_request_stats;
DROP TABLE IF EXISTS message_clicks;
