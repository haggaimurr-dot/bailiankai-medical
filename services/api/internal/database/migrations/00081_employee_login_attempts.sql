-- +goose Up
CREATE TABLE employee_login_attempts (
 id VARCHAR(64) PRIMARY KEY, username VARCHAR(64) NOT NULL,
 employee_id VARCHAR(64) NULL, success BOOLEAN NOT NULL,
 client_ip VARCHAR(64) NOT NULL DEFAULT '', created_at DATETIME(3) NOT NULL,
 INDEX login_attempt_user (username, created_at),
 FOREIGN KEY (employee_id) REFERENCES employee_accounts(id) ON DELETE SET NULL
);
-- +goose Down
DROP TABLE employee_login_attempts;
