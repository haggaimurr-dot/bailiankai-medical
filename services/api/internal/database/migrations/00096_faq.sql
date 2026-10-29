-- +goose Up
-- P2 行108: 常见问题自助问答.
-- NOTE: no COLLATE pin, no FKs (per the 00089-00093 convention: MariaDB 10.11
-- inherits utf8mb4_general_ci for new tables vs utf8mb4_unicode_ci on existing
-- ones, so FKs fail with errno 150; application-layer integrity instead).
CREATE TABLE faqs (
 id VARCHAR(64) PRIMARY KEY,
 clinic_id VARCHAR(64) NOT NULL DEFAULT '',
 category VARCHAR(64) NOT NULL DEFAULT '通用',
 question VARCHAR(500) NOT NULL,
 answer TEXT NOT NULL,
 sort_order INT NOT NULL DEFAULT 0,
 enabled BOOLEAN NOT NULL DEFAULT TRUE,
 created_by VARCHAR(64) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 KEY faqs_clinic_enabled (clinic_id, enabled, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS faqs;
