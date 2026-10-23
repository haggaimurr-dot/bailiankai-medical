-- +goose Up
CREATE TABLE care_photos (
    id VARCHAR(64) PRIMARY KEY,
    appointment_id VARCHAR(64) NOT NULL,
    patient_id VARCHAR(64) NOT NULL,
    angle VARCHAR(16) NOT NULL,
    media_type VARCHAR(32) NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    uploaded_at DATETIME(3) NOT NULL,
    UNIQUE KEY care_photos_appointment_angle (appointment_id, angle),
    KEY care_photos_patient (patient_id, appointment_id),
    CONSTRAINT fk_care_photos_appointment FOREIGN KEY (appointment_id) REFERENCES appointments(id),
    CONSTRAINT fk_care_photos_patient FOREIGN KEY (patient_id) REFERENCES patients(id),
    CONSTRAINT chk_care_photo_angle CHECK (angle IN ('front', 'left', 'right')),
    CONSTRAINT chk_care_photo_size CHECK (size_bytes > 0 AND size_bytes <= 8388608)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE photo_consent_events (
    id VARCHAR(64) PRIMARY KEY,
    appointment_id VARCHAR(64) NOT NULL,
    patient_id VARCHAR(64) NOT NULL,
    action VARCHAR(16) NOT NULL,
    version VARCHAR(32) NOT NULL,
    recorded_at DATETIME(3) NOT NULL,
    KEY photo_consent_latest (appointment_id, patient_id, recorded_at, id),
    CONSTRAINT fk_photo_consent_appointment FOREIGN KEY (appointment_id) REFERENCES appointments(id),
    CONSTRAINT fk_photo_consent_patient FOREIGN KEY (patient_id) REFERENCES patients(id),
    CONSTRAINT chk_photo_consent_action CHECK (action IN ('granted', 'revoked'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE care_plans (
    id VARCHAR(64) PRIMARY KEY,
    appointment_id VARCHAR(64) NOT NULL UNIQUE,
    draft_version INT NOT NULL DEFAULT 0,
    draft_json JSON NULL,
    updated_at DATETIME(3) NOT NULL,
    CONSTRAINT fk_care_plans_appointment FOREIGN KEY (appointment_id) REFERENCES appointments(id),
    CONSTRAINT chk_care_plan_draft_version CHECK (draft_version >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE care_plan_versions (
    id VARCHAR(64) PRIMARY KEY,
    care_plan_id VARCHAR(64) NOT NULL,
    version INT NOT NULL,
    content_json JSON NOT NULL,
    change_reason VARCHAR(300) NOT NULL,
    published_by VARCHAR(64) NOT NULL,
    published_at DATETIME(3) NOT NULL,
    UNIQUE KEY care_plan_versions_plan_version (care_plan_id, version),
    KEY care_plan_versions_timeline (care_plan_id, published_at),
    CONSTRAINT fk_care_plan_versions_plan FOREIGN KEY (care_plan_id) REFERENCES care_plans(id),
    CONSTRAINT chk_care_plan_version CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE care_access_events (
    id VARCHAR(64) PRIMARY KEY,
    appointment_id VARCHAR(64) NOT NULL,
    patient_id VARCHAR(64) NOT NULL,
    resource_type VARCHAR(24) NOT NULL,
    resource_id VARCHAR(64) NOT NULL,
    action VARCHAR(24) NOT NULL,
    actor_type VARCHAR(16) NOT NULL,
    actor_id VARCHAR(64) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    KEY care_access_timeline (appointment_id, created_at),
    KEY care_access_patient (patient_id, created_at),
    CONSTRAINT fk_care_access_appointment FOREIGN KEY (appointment_id) REFERENCES appointments(id),
    CONSTRAINT fk_care_access_patient FOREIGN KEY (patient_id) REFERENCES patients(id),
    CONSTRAINT chk_care_access_resource CHECK (resource_type IN ('photo', 'photo-set', 'care-plan')),
    CONSTRAINT chk_care_access_action CHECK (action IN ('viewed', 'listed', 'uploaded', 'draft-saved', 'published')),
    CONSTRAINT chk_care_access_actor CHECK (actor_type IN ('patient', 'clinician'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE care_access_events;
DROP TABLE care_plan_versions;
DROP TABLE care_plans;
DROP TABLE photo_consent_events;
DROP TABLE care_photos;
