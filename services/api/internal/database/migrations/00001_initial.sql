-- +goose Up
CREATE TABLE clinics (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    address VARCHAR(255) NOT NULL,
    phone VARCHAR(32) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE doctors (
    id VARCHAR(64) PRIMARY KEY,
    clinic_id VARCHAR(64) NOT NULL,
    name VARCHAR(80) NOT NULL,
    title VARCHAR(80) NOT NULL,
    summary VARCHAR(500) NOT NULL,
    focus JSON NOT NULL,
    initials VARCHAR(16) NOT NULL,
    UNIQUE KEY doctors_clinic_id (clinic_id, id),
    CONSTRAINT fk_doctors_clinic FOREIGN KEY (clinic_id) REFERENCES clinics(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE slots (
    id VARCHAR(64) PRIMARY KEY,
    clinic_id VARCHAR(64) NOT NULL,
    doctor_id VARCHAR(64) NOT NULL,
    starts_at DATETIME(3) NOT NULL,
    ends_at DATETIME(3) NOT NULL,
    capacity INT NOT NULL,
    reserved INT NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'open',
    version INT NOT NULL DEFAULT 1,
    UNIQUE KEY slots_doctor_start (doctor_id, starts_at),
    KEY slots_clinic_date (clinic_id, starts_at, doctor_id),
    CONSTRAINT fk_slots_doctor_clinic FOREIGN KEY (clinic_id, doctor_id) REFERENCES doctors(clinic_id, id),
    CONSTRAINT chk_slot_capacity CHECK (capacity >= 0 AND capacity <= 100 AND reserved >= 0 AND capacity >= reserved),
    CONSTRAINT chk_slot_times CHECK (ends_at > starts_at),
    CONSTRAINT chk_slot_status CHECK (status IN ('open', 'closed')),
    CONSTRAINT chk_slot_version CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE slots;
DROP TABLE doctors;
DROP TABLE clinics;
