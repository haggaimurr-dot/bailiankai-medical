-- +goose Up
ALTER TABLE clinics ADD active BOOLEAN NOT NULL DEFAULT TRUE, ADD version INT NOT NULL DEFAULT 1,
 ADD published_json JSON NULL, ADD published_at DATETIME(3) NULL;
ALTER TABLE doctors ADD active BOOLEAN NOT NULL DEFAULT TRUE, ADD version INT NOT NULL DEFAULT 1,
 ADD published_json JSON NULL, ADD published_at DATETIME(3) NULL,
 ADD avatar_url VARCHAR(2048) NOT NULL DEFAULT '', ADD service_introduction TEXT NOT NULL;
UPDATE clinics SET published_json = JSON_OBJECT('id',id,'name',name,'address',address,'phone',phone), published_at = NOW(3);
UPDATE doctors SET published_json = JSON_OBJECT('id',id,'clinicId',clinic_id,'name',name,'title',title,'summary',summary,'focus',focus,'initials',initials,'avatarUrl','','serviceIntroduction',''), published_at = NOW(3);
CREATE TABLE directory_revisions (
 id VARCHAR(64) PRIMARY KEY, kind VARCHAR(16) NOT NULL, resource_id VARCHAR(64) NOT NULL,
 version INT NOT NULL, action VARCHAR(32) NOT NULL, content_json JSON NOT NULL,
 actor_id VARCHAR(64) NOT NULL, created_at DATETIME(3) NOT NULL,
 UNIQUE KEY directory_resource_version(kind, resource_id, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
INSERT INTO directory_revisions SELECT UUID(),'clinics',id,version,'migrated',JSON_OBJECT('profile',published_json,'active',active),'system',NOW(3) FROM clinics;
INSERT INTO directory_revisions SELECT UUID(),'doctors',id,version,'migrated',JSON_OBJECT('profile',published_json,'active',active),'system',NOW(3) FROM doctors;
ALTER TABLE appointments ADD clinic_name_snapshot VARCHAR(120) NULL, ADD doctor_name_snapshot VARCHAR(80) NULL;
UPDATE appointments a JOIN slots s ON s.id=a.slot_id JOIN clinics c ON c.id=s.clinic_id JOIN doctors d ON d.id=s.doctor_id
 SET a.clinic_name_snapshot=c.name, a.doctor_name_snapshot=d.name;

-- +goose Down
ALTER TABLE appointments DROP clinic_name_snapshot, DROP doctor_name_snapshot;
DROP TABLE directory_revisions;
ALTER TABLE doctors DROP active, DROP version, DROP published_json, DROP published_at, DROP avatar_url, DROP service_introduction;
ALTER TABLE clinics DROP active, DROP version, DROP published_json, DROP published_at;
