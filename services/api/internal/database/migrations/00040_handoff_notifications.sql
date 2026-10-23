-- +goose Up
CREATE TABLE handoff_notifications (
 id VARCHAR(64) PRIMARY KEY,
 receipt_id VARCHAR(64) NOT NULL,
 employee_id VARCHAR(64) NOT NULL,
 kind VARCHAR(20) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 read_at DATETIME(3) NULL,
 UNIQUE KEY handoff_notification_once (receipt_id, employee_id, kind),
 KEY handoff_notification_inbox (employee_id, read_at, created_at),
 FOREIGN KEY (receipt_id) REFERENCES followup_handoff_receipts(id) ON DELETE CASCADE,
 FOREIGN KEY (employee_id) REFERENCES employee_accounts(id) ON DELETE CASCADE,
 CHECK (kind IN ('assigned', 'escalated'))
);

-- +goose Down
DROP TABLE handoff_notifications;
