-- +goose Up
CREATE TABLE followup_handoff_settings (
 id TINYINT PRIMARY KEY,
 overdue_hours INT NOT NULL,
 version INT NOT NULL,
 updated_by VARCHAR(64) NULL,
 updated_at DATETIME(3) NOT NULL,
 CHECK (id = 1),
 CHECK (overdue_hours BETWEEN 1 AND 720),
 CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
INSERT INTO followup_handoff_settings (id, overdue_hours, version, updated_at) VALUES (1, 24, 1, UTC_TIMESTAMP(3));

-- +goose Down
DROP TABLE followup_handoff_settings;
