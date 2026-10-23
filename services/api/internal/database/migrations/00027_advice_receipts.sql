-- +goose Up
CREATE TABLE advice_receipts (
 patient_id VARCHAR(64) NOT NULL,
 kind VARCHAR(16) NOT NULL,
 resource_id VARCHAR(64) NOT NULL,
 advice_version CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 read_at DATETIME(3) NOT NULL,
 PRIMARY KEY (patient_id, kind, resource_id, advice_version),
 CONSTRAINT fk_advice_receipt_patient FOREIGN KEY (patient_id) REFERENCES patients(id),
 CONSTRAINT chk_advice_receipt_kind CHECK (kind IN ('initial', 'revisit', 'plan'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE advice_receipts;
