-- +goose Up
-- 医嘱待办（P2 方案-患者端）：患者确认方案医嘱后按行生成的随访待办没有
-- 对应的实际治疗记录，treatment_id 须允许为空；为空时仍受外键约束
-- （NULL 通过外键检查），且 (treatment_id, due_at) 唯一键下多个 NULL
-- 互不冲突。不要在这里 pin COLLATE（00074/00075 的教训）。
ALTER TABLE followup_tasks MODIFY treatment_id VARCHAR(64) NULL;

-- +goose Down
ALTER TABLE followup_tasks MODIFY treatment_id VARCHAR(64) NOT NULL;
