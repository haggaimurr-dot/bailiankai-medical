-- +goose Up
ALTER TABLE followup_handoff_receipts
 ADD COLUMN followup_status VARCHAR(20) NOT NULL DEFAULT 'untracked',
 ADD COLUMN followup_version INT NOT NULL DEFAULT 1,
 ADD KEY handoff_followup_status (followup_status, status, created_at),
 ADD CONSTRAINT handoff_followup_status_check CHECK (followup_status IN ('untracked', 'awaiting_reply', 'needs_action', 'resolved')),
 ADD CONSTRAINT handoff_followup_version_check CHECK (followup_version > 0);

ALTER TABLE handoff_followup_events
 ADD COLUMN status_after VARCHAR(20) NOT NULL DEFAULT 'needs_action',
 ADD CONSTRAINT handoff_followup_event_status_check CHECK (status_after IN ('awaiting_reply', 'needs_action', 'resolved'));

UPDATE followup_handoff_receipts r
SET r.followup_status = 'needs_action',
    r.followup_version = 1 + (SELECT COUNT(*) FROM handoff_followup_events e WHERE e.receipt_id = r.id)
WHERE EXISTS (SELECT 1 FROM handoff_followup_events e WHERE e.receipt_id = r.id);

-- +goose Down
ALTER TABLE handoff_followup_events DROP CHECK handoff_followup_event_status_check, DROP COLUMN status_after;
ALTER TABLE followup_handoff_receipts DROP CHECK handoff_followup_status_check, DROP CHECK handoff_followup_version_check, DROP INDEX handoff_followup_status, DROP COLUMN followup_status, DROP COLUMN followup_version;
