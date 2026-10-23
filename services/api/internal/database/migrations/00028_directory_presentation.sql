-- +goose Up
ALTER TABLE clinics ADD presentation JSON NULL;
ALTER TABLE doctors ADD presentation JSON NULL;

-- +goose Down
ALTER TABLE doctors DROP presentation;
ALTER TABLE clinics DROP presentation;
