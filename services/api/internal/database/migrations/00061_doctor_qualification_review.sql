-- +goose Up
ALTER TABLE doctors ADD qualification_review JSON NULL;

-- +goose Down
ALTER TABLE doctors DROP qualification_review;
