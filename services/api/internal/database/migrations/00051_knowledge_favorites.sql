-- +goose Up
CREATE TABLE IF NOT EXISTS knowledge_favorites (
 patient_id VARCHAR(64) NOT NULL,
 article_id VARCHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 PRIMARY KEY (patient_id, article_id),
 FOREIGN KEY (patient_id) REFERENCES patients(id),
 FOREIGN KEY (article_id) REFERENCES knowledge_articles(id)
);

-- +goose Down
DROP TABLE knowledge_favorites;
