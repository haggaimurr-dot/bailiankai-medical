-- +goose Up
-- AI用途授权文案分版本管理（P2-8）：文案与版本入库，主管在工作台可发布新版；
-- 患者之前按旧版给的授权在新版发布后自动失效，须重新确认。
-- 注意：不要在这里 pin COLLATE（00074/00075 的教训：CI 全新库默认 utf8mb4_0900_ai_ci，
-- 上海库默认 utf8mb4_unicode_ci，显式 COLLATE 会导致外键 3780；本表无外键，各环境按库默认建表）。
CREATE TABLE IF NOT EXISTS assessment_consent_policies (
  id VARCHAR(64) NOT NULL PRIMARY KEY,
  version VARCHAR(64) NOT NULL UNIQUE,
  content TEXT NOT NULL,
  created_by VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  INDEX idx_assessment_consent_policies_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT INTO assessment_consent_policies (id, version, content, created_by, created_at)
VALUES ('acp-seed-v1', 'photo-ai-v1', 'AI照片辅助观察授权（试点草案）：您选择的正面、本人左侧和右侧照片仅用于本次AI辅助观察及医生复核，处理可能由配置的模型服务提供方完成。AI结果存在局限，医生复核后才向您展示，不作为诊断或治疗处方。您可撤回授权，撤回后停止后续分析和医护访问；已发送至服务提供方的数据不能立即追回。正式服务主体、提供方、保存期限和删除渠道须由院方确认。此授权独立于隐私说明、诊疗照片授权及案例展示授权。', '', NOW(3))
ON DUPLICATE KEY UPDATE version = VALUES(version);

-- +goose Down
DROP TABLE IF EXISTS assessment_consent_policies;
