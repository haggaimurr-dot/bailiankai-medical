-- +goose Up
CREATE TABLE message_templates (
 id VARCHAR(64) PRIMARY KEY, clinic_id VARCHAR(64) NOT NULL, kind VARCHAR(20) NOT NULL,
 template_id VARCHAR(100) NOT NULL, title_key VARCHAR(32) NOT NULL, time_key VARCHAR(32) NOT NULL,
 page VARCHAR(200) NOT NULL, state VARCHAR(16) NOT NULL, lead_minutes INT NOT NULL,
 enabled BOOLEAN NOT NULL, version INT NOT NULL,
 UNIQUE KEY message_template_kind (clinic_id, kind), FOREIGN KEY (clinic_id) REFERENCES clinics(id)
);
CREATE TABLE message_consents (
 patient_id VARCHAR(64) NOT NULL, template_id VARCHAR(100) NOT NULL, status VARCHAR(20) NOT NULL,
 updated_at DATETIME(3) NOT NULL, PRIMARY KEY (patient_id, template_id), FOREIGN KEY (patient_id) REFERENCES patients(id)
);
CREATE TABLE message_consent_events (
 id VARCHAR(64) PRIMARY KEY, patient_id VARCHAR(64) NOT NULL, template_id VARCHAR(100) NOT NULL,
 request_key VARCHAR(80) NOT NULL, status VARCHAR(20) NOT NULL, created_at DATETIME(3) NOT NULL,
 UNIQUE KEY message_consent_request (patient_id, request_key), FOREIGN KEY (patient_id) REFERENCES patients(id)
);
CREATE TABLE notifications (
 id VARCHAR(64) PRIMARY KEY, clinic_id VARCHAR(64) NOT NULL, patient_id VARCHAR(64) NOT NULL,
 template_id VARCHAR(64) NOT NULL, kind VARCHAR(20) NOT NULL, source_id VARCHAR(64) NOT NULL,
 due_at DATETIME(3) NOT NULL, send_at DATETIME(3) NOT NULL, status VARCHAR(20) NOT NULL,
 attempts INT NOT NULL DEFAULT 0, last_code VARCHAR(40) NOT NULL DEFAULT '', updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY notification_source (kind, source_id, due_at), INDEX notification_dispatch (status, send_at),
 FOREIGN KEY (clinic_id) REFERENCES clinics(id), FOREIGN KEY (patient_id) REFERENCES patients(id),
 FOREIGN KEY (template_id) REFERENCES message_templates(id)
);
CREATE TABLE notification_attempts (
 id VARCHAR(64) PRIMARY KEY, notification_id VARCHAR(64) NOT NULL, status VARCHAR(20) NOT NULL,
 code VARCHAR(40) NOT NULL, created_at DATETIME(3) NOT NULL,
 FOREIGN KEY (notification_id) REFERENCES notifications(id), INDEX notification_attempt_order (notification_id, created_at)
);
-- +goose Down
DROP TABLE notification_attempts;
DROP TABLE notifications;
DROP TABLE message_consent_events;
DROP TABLE message_consents;
DROP TABLE message_templates;
