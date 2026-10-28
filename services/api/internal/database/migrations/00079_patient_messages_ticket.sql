-- +goose Up
-- 咨询回复站内提醒（P2 咨询-患者端）：客服回复工单时写一条站内消息，
-- ticket_id 关联工单，患者进工单详情时将该工单的未读回复消息标已读。
-- 不 pin COLLATE（00074/00075 的教训）；与 appointment_id 同款 NOT NULL DEFAULT ''。
ALTER TABLE patient_messages ADD COLUMN ticket_id VARCHAR(64) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE patient_messages DROP COLUMN ticket_id;
