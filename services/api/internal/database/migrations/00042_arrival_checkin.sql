-- +goose Up
CREATE TABLE check_in_codes (
 token_hash VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 appointment_id VARCHAR(64) NOT NULL,
 appointment_version INT NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 used_at DATETIME(3) NULL,
 KEY check_in_expiry (expires_at),
 CONSTRAINT check_in_appointment FOREIGN KEY (appointment_id) REFERENCES appointments(id)
);

-- +goose Down
DROP TABLE check_in_codes;
