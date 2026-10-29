-- +goose Up
-- 渠道来源：预约创建时由患者端记录的站内入站来源（direct/home/cases/doctors/
-- knowledge/share），只记站内路径与分享标记，不采外部隐私数据；历史预约
-- 为空，漏斗里按“未记录”归组。不要在这里 pin COLLATE（00074/00075 的教训）。
ALTER TABLE appointments ADD COLUMN source VARCHAR(32) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE appointments DROP COLUMN source;
