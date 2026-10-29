-- +goose Up
-- 取消原因收集：患者取消时可附原因（枚举）与补充说明，取消时间单独记录，
-- 供后台按区间/院区/医生统计取消原因。不填原因不阻断取消。
ALTER TABLE appointments
 ADD COLUMN cancel_reason VARCHAR(32) NULL,
 ADD COLUMN cancel_reason_detail VARCHAR(200) NULL,
 ADD COLUMN cancelled_at DATETIME(3) NULL,
 ADD INDEX idx_appointments_cancel_stats (status, cancelled_at);
-- +goose Down
ALTER TABLE appointments DROP INDEX idx_appointments_cancel_stats;
ALTER TABLE appointments
 DROP COLUMN cancelled_at,
 DROP COLUMN cancel_reason_detail,
 DROP COLUMN cancel_reason;
