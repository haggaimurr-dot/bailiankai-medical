-- +goose Up
-- 新增-P2 行 102：模型效果抽检与人工评分回流。抽检已复核发布的评估，人工打分，
-- 报表按模型版本汇总，可下钻到单条。
CREATE TABLE assessment_quality_scores (
    id VARCHAR(64) PRIMARY KEY,
    assessment_id VARCHAR(64) NOT NULL,
    scorer_employee_id VARCHAR(64) NOT NULL,
    score TINYINT NOT NULL,
    comment VARCHAR(1000) NOT NULL DEFAULT '',
    model_version VARCHAR(200) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL,
    UNIQUE KEY uq_assessment_scorer (assessment_id, scorer_employee_id),
    KEY idx_model_version (model_version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
