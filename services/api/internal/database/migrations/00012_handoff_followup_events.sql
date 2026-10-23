-- +goose Up
CREATE TABLE handoff_followup_events (
 id VARCHAR(64) PRIMARY KEY,
 receipt_id VARCHAR(64) NOT NULL,
 request_key VARCHAR(64) NOT NULL,
 action_text VARCHAR(500) NOT NULL,
 result_text VARCHAR(1000) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 UNIQUE KEY handoff_followup_request (request_key),
 KEY handoff_followup_receipt (receipt_id, created_at, id),
 FOREIGN KEY (receipt_id) REFERENCES followup_handoff_receipts(id),
 FOREIGN KEY (actor_id) REFERENCES employee_accounts(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE handoff_followup_events;
