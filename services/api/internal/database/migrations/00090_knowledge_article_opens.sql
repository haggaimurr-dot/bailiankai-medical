-- +goose Up
-- 知识库文章阅读打开事件：患者端详情页打开即打点（fire-and-forget），只统计已发布
-- 快照的打开（草稿/未发布文章不记）。patient_id 为空表示未登录打开。不要在这里 pin COLLATE。
CREATE TABLE knowledge_article_opens (
 id VARCHAR(64) PRIMARY KEY,
 article_id VARCHAR(64) NOT NULL,
 clinic_id VARCHAR(64) NOT NULL DEFAULT '',
 patient_id VARCHAR(64) NULL,
 opened_at DATETIME(6) NOT NULL,
 INDEX idx_knowledge_opens_article (article_id, opened_at),
 INDEX idx_knowledge_opens_clinic (clinic_id, opened_at)
);

-- +goose Down
DROP TABLE knowledge_article_opens;
