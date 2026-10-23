-- +goose Up
CREATE TABLE consultations (
    id VARCHAR(64) PRIMARY KEY,
    patient_id VARCHAR(64) NOT NULL,
    chief_complaint VARCHAR(1000) NOT NULL,
    history TEXT NOT NULL,
    assessment TEXT NOT NULL,
    advice TEXT NOT NULL,
    recorded_by VARCHAR(64) NOT NULL,
    recorded_at DATETIME(3) NOT NULL,
    KEY consultations_patient_time (patient_id, recorded_at),
    CONSTRAINT fk_consultations_patient FOREIGN KEY (patient_id) REFERENCES patients(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE consultations;
