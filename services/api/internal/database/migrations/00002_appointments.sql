-- +goose Up
CREATE TABLE patients (
    id VARCHAR(64) PRIMARY KEY,
    display_name VARCHAR(40) NOT NULL,
    created_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE sessions (
    token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    patient_id VARCHAR(64) NOT NULL,
    expires_at DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    KEY sessions_patient (patient_id),
    KEY sessions_expiry (expires_at),
    CONSTRAINT fk_sessions_patient FOREIGN KEY (patient_id) REFERENCES patients(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE appointments (
    id VARCHAR(64) PRIMARY KEY,
    patient_id VARCHAR(64) NOT NULL,
    slot_id VARCHAR(64) NOT NULL,
    contact_name VARCHAR(40) NOT NULL,
    contact_phone VARCHAR(11) NOT NULL,
    note VARCHAR(300) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL,
    rejection_reason VARCHAR(200) NOT NULL DEFAULT '',
    confirmation_deadline DATETIME(3) NOT NULL,
    request_key VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    version INT NOT NULL DEFAULT 1,
    active_slot_id VARCHAR(64) GENERATED ALWAYS AS (CASE WHEN status IN ('pending', 'confirmed') THEN slot_id ELSE NULL END) STORED,
    UNIQUE KEY appointments_request (patient_id, request_key),
    UNIQUE KEY appointments_active_patient_slot (patient_id, active_slot_id),
    KEY appointments_pending_expiry (status, confirmation_deadline),
    KEY appointments_patient_date (patient_id, created_at, id),
    KEY appointments_slot (slot_id),
    CONSTRAINT fk_appointments_patient FOREIGN KEY (patient_id) REFERENCES patients(id),
    CONSTRAINT fk_appointments_slot FOREIGN KEY (slot_id) REFERENCES slots(id),
    CONSTRAINT chk_appointment_status CHECK (status IN ('pending', 'confirmed', 'rejected', 'cancelled', 'expired')),
    CONSTRAINT chk_appointment_version CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE appointment_events (
    id VARCHAR(64) PRIMARY KEY,
    appointment_id VARCHAR(64) NOT NULL,
    actor_type VARCHAR(16) NOT NULL,
    actor_id VARCHAR(64) NOT NULL,
    action VARCHAR(16) NOT NULL,
    from_status VARCHAR(16) NOT NULL,
    to_status VARCHAR(16) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    KEY appointment_events_timeline (appointment_id, created_at),
    CONSTRAINT fk_appointment_events_appointment FOREIGN KEY (appointment_id) REFERENCES appointments(id),
    CONSTRAINT chk_event_actor CHECK (actor_type IN ('patient', 'operator', 'system'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE appointment_events;
DROP TABLE appointments;
DROP TABLE sessions;
DROP TABLE patients;
