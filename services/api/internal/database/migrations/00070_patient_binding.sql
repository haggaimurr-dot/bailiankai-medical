-- +goose Up
CREATE TABLE patient_binding_codes (
 token_hash VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 patient_id VARCHAR(64) NOT NULL,
 patient_version INT NOT NULL,
 created_by VARCHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 consumed_at DATETIME(3) NULL,
 source_patient_id VARCHAR(64) NOT NULL DEFAULT '',
 KEY binding_patient (patient_id),
 FOREIGN KEY (patient_id) REFERENCES patients(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
-- +goose Down
DROP TABLE patient_binding_codes;
