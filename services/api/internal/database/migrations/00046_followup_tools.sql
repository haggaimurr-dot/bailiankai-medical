-- +goose Up
CREATE TABLE IF NOT EXISTS followup_templates (
 id VARCHAR(64) PRIMARY KEY, clinic_id VARCHAR(64) NOT NULL,
 name VARCHAR(100) NOT NULL, offsets JSON NOT NULL,
 response VARCHAR(2000) NOT NULL, disposition VARCHAR(1000) NOT NULL,
 active BOOLEAN NOT NULL, version INT NOT NULL, updated_at DATETIME(3) NOT NULL,
 FOREIGN KEY (clinic_id) REFERENCES clinics(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE IF NOT EXISTS followup_template_events (
 id VARCHAR(64) PRIMARY KEY, template_id VARCHAR(64) NOT NULL,
 actor_id VARCHAR(64) NOT NULL, content_json JSON NOT NULL, created_at DATETIME(3) NOT NULL,
 FOREIGN KEY (template_id) REFERENCES followup_templates(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE IF NOT EXISTS nursing_feedbacks (
 id VARCHAR(64) PRIMARY KEY, appointment_id VARCHAR(64) NOT NULL,
 request_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 observed_at DATETIME(3) NOT NULL, content VARCHAR(2000) NOT NULL, action VARCHAR(1000) NOT NULL,
 actor_id VARCHAR(64) NOT NULL, actor_name VARCHAR(100) NOT NULL, created_at DATETIME(3) NOT NULL,
 UNIQUE KEY nursing_request (actor_id, request_key), INDEX nursing_appointment (appointment_id, observed_at),
 FOREIGN KEY (appointment_id) REFERENCES appointments(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
-- +goose Down
DROP TABLE nursing_feedbacks;
DROP TABLE followup_template_events;
DROP TABLE followup_templates;
