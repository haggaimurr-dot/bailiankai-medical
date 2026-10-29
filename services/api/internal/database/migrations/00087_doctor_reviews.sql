-- +goose Up
-- 医生患者评价：仅已治疗（treatments 有记录）患者可评，每单（预约）一次；
-- 评价先待审核，管理端审核通过后才向患者端公开显示。
CREATE TABLE doctor_reviews (
 id VARCHAR(64) PRIMARY KEY,
 doctor_id VARCHAR(64) NOT NULL,
 patient_id VARCHAR(64) NOT NULL,
 appointment_id VARCHAR(64) NOT NULL,
 rating INT NOT NULL,
 content VARCHAR(1000) NOT NULL,
 status VARCHAR(16) NOT NULL,
 review_note VARCHAR(500) NULL,
 created_at DATETIME(3) NOT NULL,
 reviewed_at DATETIME(3) NULL,
 INDEX idx_doctor_reviews_doctor (doctor_id, status, created_at),
 INDEX idx_doctor_reviews_patient (patient_id, created_at),
 UNIQUE INDEX idx_doctor_reviews_appointment (appointment_id)
);
-- +goose Down
DROP TABLE doctor_reviews;
