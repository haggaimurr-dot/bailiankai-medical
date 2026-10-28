-- +goose Up
-- 候补限时确认：空位释放时给最早一位候补患者发限时确认 offer，超时自动顺延给下一位。
-- offer 持有期间名额计入 slot.reserved（与正常预约同一计数），确认后名额归属该预约，
-- 过期/失效时释放并顺延。seat 计数与 offer 状态在同一事务内变更，不存在悬空持有。
CREATE TABLE waitlist_offers (
 id VARCHAR(64) PRIMARY KEY,
 appointment_id VARCHAR(64) NOT NULL,
 slot_id VARCHAR(64) NOT NULL,
 patient_id VARCHAR(64) NOT NULL,
 status VARCHAR(32) NOT NULL DEFAULT 'offered',
 expires_at DATETIME(3) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 INDEX idx_waitlist_offers_slot (slot_id, status, created_at),
 INDEX idx_waitlist_offers_appointment (appointment_id, status)
);
-- +goose Down
DROP TABLE waitlist_offers;
