-- +goose Up
ALTER TABLE knowledge_articles ADD COLUMN source_text VARCHAR(5000) NOT NULL DEFAULT '' AFTER body;
CREATE INDEX knowledge_article_review ON knowledge_articles (review_status, category_id);

-- +goose Down
DROP INDEX knowledge_article_review ON knowledge_articles;
ALTER TABLE knowledge_articles DROP COLUMN source_text;
