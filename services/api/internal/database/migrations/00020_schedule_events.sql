-- +goose Up
CREATE TABLE schedule_events (
 id VARCHAR(64) PRIMARY KEY,
 slot_id VARCHAR(64) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 action VARCHAR(32) NOT NULL,
 reason VARCHAR(200) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL,
 KEY schedule_slot_events (slot_id, created_at),
 CONSTRAINT fk_schedule_event_slot FOREIGN KEY (slot_id) REFERENCES slots(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE schedule_events;
