-- +goose Up
-- 超时未确认预约的自动升级提醒：待确认预约临期（确认截止前 30 分钟内）
-- 给可审核员工（主管、客服）发 pending_soon 提醒；已因超时未确认而过期
-- 的预约给授权主管发 pending_escalated 升级提醒。通知 id 由预约 + 员工 +
-- 类型哈希确定，重复扫表幂等，不另建去重表。
CREATE TABLE appointment_notifications (
 id VARCHAR(64) PRIMARY KEY,
 appointment_id VARCHAR(64) NOT NULL,
 employee_id VARCHAR(64) NOT NULL,
 kind VARCHAR(32) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 read_at DATETIME(3) NULL,
 INDEX idx_appt_notifications_employee (employee_id, created_at),
 INDEX idx_appt_notifications_appointment (appointment_id, kind)
);
-- +goose Down
DROP TABLE appointment_notifications;
