-- +goose Up
CREATE TABLE employee_accounts (
 id VARCHAR(64) PRIMARY KEY,
 username VARCHAR(80) NOT NULL UNIQUE,
 display_name VARCHAR(80) NOT NULL,
 password_hash VARCHAR(100) NOT NULL,
 role VARCHAR(16) NOT NULL,
 staff_id VARCHAR(64) NULL UNIQUE,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 version INT NOT NULL DEFAULT 1,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 FOREIGN KEY (staff_id) REFERENCES followup_staff(id),
 CHECK (role IN ('manager','clinician')),
 CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE employee_sessions (
 token_hash CHAR(64) PRIMARY KEY,
 employee_id VARCHAR(64) NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 revoked_at DATETIME(3) NULL,
 created_at DATETIME(3) NOT NULL,
 KEY employee_sessions_employee (employee_id, expires_at),
 FOREIGN KEY (employee_id) REFERENCES employee_accounts(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE employee_sessions;
DROP TABLE employee_accounts;
