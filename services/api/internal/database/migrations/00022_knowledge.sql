-- +goose Up
CREATE TABLE knowledge_categories (
 id VARCHAR(64) PRIMARY KEY,
 clinic_id VARCHAR(64) NOT NULL,
 name VARCHAR(120) NOT NULL,
 sort_order INT NOT NULL DEFAULT 0,
 active BOOLEAN NOT NULL DEFAULT TRUE,
 version INT NOT NULL DEFAULT 1,
 FOREIGN KEY (clinic_id) REFERENCES clinics(id),
 INDEX knowledge_category_order (clinic_id, active, sort_order, id)
);
CREATE TABLE knowledge_articles (
 id VARCHAR(64) PRIMARY KEY,
 category_id VARCHAR(64) NOT NULL,
 title VARCHAR(200) NOT NULL,
 summary VARCHAR(1000) NOT NULL,
 body LONGTEXT NOT NULL,
 sort_order INT NOT NULL DEFAULT 0,
 version INT NOT NULL DEFAULT 1,
 review_status VARCHAR(20) NOT NULL DEFAULT 'pending',
 review_note VARCHAR(1000) NOT NULL DEFAULT '',
 reviewer_id VARCHAR(64) NOT NULL DEFAULT '',
 reviewer_name VARCHAR(120) NOT NULL DEFAULT '',
 reviewed_at DATETIME(3) NULL,
 published_json JSON NULL,
 published_at DATETIME(3) NULL,
 FOREIGN KEY (category_id) REFERENCES knowledge_categories(id),
 INDEX knowledge_article_order (category_id, sort_order, id)
);
CREATE TABLE knowledge_events (
 id VARCHAR(64) PRIMARY KEY,
 kind VARCHAR(20) NOT NULL,
 resource_id VARCHAR(64) NOT NULL,
 version INT NOT NULL,
 action VARCHAR(20) NOT NULL,
 actor_id VARCHAR(64) NOT NULL,
 content_json JSON NOT NULL,
 created_at DATETIME(3) NOT NULL,
 UNIQUE KEY knowledge_event_version (kind, resource_id, version)
);

-- +goose Down
DROP TABLE knowledge_events;
DROP TABLE knowledge_articles;
DROP TABLE knowledge_categories;
