-- +goose Up
CREATE TABLE support_tickets (
 id VARCHAR(64) PRIMARY KEY,
 patient_id VARCHAR(64) NOT NULL,
 clinic_id VARCHAR(64) NOT NULL,
 subject VARCHAR(100) NOT NULL,
 category VARCHAR(30) NOT NULL,
 status VARCHAR(30) NOT NULL,
 assignee_id VARCHAR(64) NOT NULL DEFAULT '',
 due_at DATETIME(3) NULL,
 version INT NOT NULL DEFAULT 1,
 request_key VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 request_hash VARCHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY support_patient_request(patient_id, request_key),
 KEY support_queue(clinic_id,status,updated_at),
 KEY support_assignee(assignee_id,due_at),
 CONSTRAINT fk_support_patient FOREIGN KEY(patient_id) REFERENCES patients(id),
 CONSTRAINT fk_support_clinic FOREIGN KEY(clinic_id) REFERENCES clinics(id),
 CONSTRAINT chk_support_status CHECK(status IN ('open','in_progress','waiting_patient','resolved','closed'))
);
CREATE TABLE support_events (
 id VARCHAR(64) PRIMARY KEY,
 ticket_id VARCHAR(64) NOT NULL,
 kind VARCHAR(30) NOT NULL,
 actor_type VARCHAR(20) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 content TEXT NOT NULL,
 internal BOOLEAN NOT NULL DEFAULT FALSE,
 request_key VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 request_hash VARCHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 UNIQUE KEY support_event_request(ticket_id,actor_id,request_key),
 KEY support_event_history(ticket_id,created_at,id),
 CONSTRAINT fk_support_event_ticket FOREIGN KEY(ticket_id) REFERENCES support_tickets(id)
);
-- +goose Down
DROP TABLE support_events;
DROP TABLE support_tickets;
