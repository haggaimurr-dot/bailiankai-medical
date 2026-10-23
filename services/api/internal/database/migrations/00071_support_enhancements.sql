-- +goose Up
ALTER TABLE support_tickets ADD rating INT NOT NULL DEFAULT 0,
 ADD rating_comment VARCHAR(500) NOT NULL DEFAULT '',
 ADD CONSTRAINT chk_support_rating CHECK (rating BETWEEN 0 AND 5);
CREATE TABLE support_attachments (
 id VARCHAR(64) PRIMARY KEY,
 ticket_id VARCHAR(64) NOT NULL,
 event_id VARCHAR(64) NOT NULL,
 name VARCHAR(150) NOT NULL,
 mime_type VARCHAR(100) NOT NULL,
 size INT NOT NULL,
 data MEDIUMBLOB NOT NULL,
 KEY support_attachment_event(event_id),
 CONSTRAINT fk_support_attachment_ticket FOREIGN KEY(ticket_id) REFERENCES support_tickets(id),
 CONSTRAINT fk_support_attachment_event FOREIGN KEY(event_id) REFERENCES support_events(id)
);
-- +goose Down
DROP TABLE support_attachments;
ALTER TABLE support_tickets DROP CHECK chk_support_rating, DROP rating, DROP rating_comment;
