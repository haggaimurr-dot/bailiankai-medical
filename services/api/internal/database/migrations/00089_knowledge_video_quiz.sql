-- +goose Up
-- 知识库文章短视频科普与「看完测一测」：video_url 为 HTTPS 短视频地址（空表示无视频）；
-- quiz_json 为自测题 JSON 数组（空表示无自测），结构见 model.KnowledgeQuizQuestion；
-- 两者都随发布快照进入 PublishedJSON，公开端直接透出。不要在这里 pin COLLATE。
ALTER TABLE knowledge_articles ADD COLUMN video_url VARCHAR(2048) NOT NULL DEFAULT '';
ALTER TABLE knowledge_articles ADD COLUMN quiz_json JSON NULL;

-- +goose Down
ALTER TABLE knowledge_articles DROP COLUMN quiz_json;
ALTER TABLE knowledge_articles DROP COLUMN video_url;
