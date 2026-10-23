-- +goose Up
CREATE TABLE care_plan_templates (
 id VARCHAR(64) PRIMARY KEY,
 owner_id VARCHAR(64) NOT NULL,
 name VARCHAR(80) NOT NULL,
 content_json JSON NOT NULL,
 created_at DATETIME(3) NOT NULL,
 INDEX care_template_owner (owner_id, created_at),
 FOREIGN KEY (owner_id) REFERENCES employee_accounts(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE care_plan_templates;
