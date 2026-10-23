-- +goose Up
CREATE TABLE employee_account_events (
 id VARCHAR(64) PRIMARY KEY,
 employee_id VARCHAR(64) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 action VARCHAR(24) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 KEY employee_account_history (employee_id, created_at, id),
 FOREIGN KEY (employee_id) REFERENCES employee_accounts(id),
 CHECK (action IN ('created','activated','deactivated','password-reset'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO employee_account_events (id, employee_id, actor_id, action, created_at)
SELECT UUID(), id, 'system', 'created', created_at FROM employee_accounts;

-- +goose Down
DROP TABLE employee_account_events;
