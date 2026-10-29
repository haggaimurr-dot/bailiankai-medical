-- +goose Up
-- P2 batch 3 (rows 109/110/111).
-- 行109: 档案临时授权——有时效的分享链接。
-- NOTE: no COLLATE pin, no FKs (per the 00089-00093 convention).
CREATE TABLE record_share_links (
 id VARCHAR(64) PRIMARY KEY,
 patient_id VARCHAR(64) NOT NULL,
 token_hash CHAR(64) NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 revoked BOOLEAN NOT NULL DEFAULT FALSE,
 created_by VARCHAR(64) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL,
 UNIQUE KEY record_share_links_token (token_hash),
 KEY record_share_links_patient (patient_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 行110: 患者标签分层——vip / normal / high_risk。
-- NOTE: separate table (not altering patients) to keep the migration simple
-- and avoid touching the core table; application joins on read.
CREATE TABLE patient_tiers (
 patient_id VARCHAR(64) PRIMARY KEY,
 tier VARCHAR(20) NOT NULL DEFAULT 'normal',
 reason VARCHAR(300) NOT NULL DEFAULT '',
 updated_by VARCHAR(64) NOT NULL DEFAULT '',
 updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS patient_tiers;
DROP TABLE IF EXISTS record_share_links;
