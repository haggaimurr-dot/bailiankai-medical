-- +goose Up
CREATE TABLE record_export_jobs (
 id VARCHAR(64) PRIMARY KEY,
 employee_id VARCHAR(64) NOT NULL,
 patient_count INT NOT NULL,
 sections JSON NOT NULL,
 date_from VARCHAR(10) NOT NULL DEFAULT '',
 date_to VARCHAR(10) NOT NULL DEFAULT '',
 masked BOOLEAN NOT NULL DEFAULT TRUE,
 reason VARCHAR(200) NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'queued',
 created_at DATETIME(3) NOT NULL,
 completed_at DATETIME(3) NULL,
 expires_at DATETIME(3) NULL,
 size_bytes BIGINT NOT NULL DEFAULT 0,
 error_code VARCHAR(40) NOT NULL DEFAULT '',
 download_count INT NOT NULL DEFAULT 0,
 snapshot_json JSON NOT NULL,
 payload MEDIUMBLOB NULL,
 lease_until DATETIME(3) NULL,
 lease_token VARCHAR(64) NOT NULL DEFAULT '',
 FOREIGN KEY (employee_id) REFERENCES employee_accounts(id),
 KEY export_owner (employee_id, created_at, id),
 KEY export_worker (status, lease_until, created_at),
 KEY export_expiry (status, expires_at),
 CHECK (patient_count BETWEEN 1 AND 20),
 CHECK (status IN ('queued','processing','ready','failed','expired'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE record_export_events (
 id VARCHAR(64) PRIMARY KEY,
 job_id VARCHAR(64) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 action VARCHAR(32) NOT NULL,
 detail_code VARCHAR(40) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL,
 FOREIGN KEY (job_id) REFERENCES record_export_jobs(id),
 KEY export_audit (job_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE record_export_events;
DROP TABLE record_export_jobs;
