-- +goose Up
CREATE TABLE assessments (
 id VARCHAR(64) PRIMARY KEY, patient_id VARCHAR(64) NOT NULL, clinic_id VARCHAR(64) NOT NULL,
 appointment_id VARCHAR(64) NOT NULL DEFAULT '', supersedes_assessment_id VARCHAR(64) NOT NULL DEFAULT '',
 request_key VARCHAR(64) NOT NULL, request_hash CHAR(64) NOT NULL,
 version INT NOT NULL DEFAULT 1, status VARCHAR(32) NOT NULL DEFAULT 'draft',
 consent_granted BOOLEAN NOT NULL DEFAULT FALSE, consent_event_id VARCHAR(64) NOT NULL DEFAULT '',
 reviewer_employee_id VARCHAR(64) NOT NULL DEFAULT '', snapshot_id VARCHAR(64) NOT NULL DEFAULT '',
 result_id VARCHAR(64) NOT NULL DEFAULT '', published_review_id VARCHAR(64) NOT NULL DEFAULT '',
 attempt_count INT NOT NULL DEFAULT 0, max_attempts INT NOT NULL DEFAULT 3,
 failure_code VARCHAR(100) NOT NULL DEFAULT '', retryable BOOLEAN NOT NULL DEFAULT FALSE,
 lease_token VARCHAR(64) NOT NULL DEFAULT '', lease_expires_at DATETIME(3) NULL,
 quality_requested BOOLEAN NOT NULL DEFAULT FALSE, quality_requested_at DATETIME(3) NULL,
 quality_lease_token VARCHAR(64) NOT NULL DEFAULT '', quality_lease_expires_at DATETIME(3) NULL,
 next_attempt_at DATETIME(3) NULL, submitted_at DATETIME(3) NULL, analysis_completed_at DATETIME(3) NULL, reviewed_at DATETIME(3) NULL,
 created_at DATETIME(3) NOT NULL, updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY assessment_patient_request (patient_id, request_key), KEY assessment_queue (status, next_attempt_at),
 KEY assessment_quality_queue (quality_requested, quality_lease_expires_at), KEY assessment_patient_created (patient_id, created_at),
 CONSTRAINT assessment_patient_fk FOREIGN KEY (patient_id) REFERENCES patients(id) ON DELETE CASCADE,
 CONSTRAINT assessment_clinic_fk FOREIGN KEY (clinic_id) REFERENCES clinics(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE assessment_photos (
 id VARCHAR(64) PRIMARY KEY, assessment_id VARCHAR(64) NOT NULL, angle VARCHAR(16) NOT NULL,
 media_type VARCHAR(32) NOT NULL, size_bytes BIGINT NOT NULL, width INT NOT NULL, height INT NOT NULL, sha256 CHAR(64) NOT NULL,
 is_current BOOLEAN NOT NULL, is_frozen BOOLEAN NOT NULL DEFAULT FALSE, quality_json JSON NOT NULL, uploaded_at DATETIME(3) NOT NULL,
 current_angle VARCHAR(16) GENERATED ALWAYS AS (CASE WHEN is_current THEN angle ELSE NULL END) STORED,
 UNIQUE KEY assessment_current_angle (assessment_id, current_angle),
 CONSTRAINT assessment_photo_fk FOREIGN KEY (assessment_id) REFERENCES assessments(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE assessment_input_snapshots (
 id VARCHAR(64) PRIMARY KEY, assessment_id VARCHAR(64) NOT NULL, content_json JSON NOT NULL, created_at DATETIME(3) NOT NULL,
 UNIQUE KEY assessment_snapshot (assessment_id), CONSTRAINT assessment_input_fk FOREIGN KEY (assessment_id) REFERENCES assessments(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE assessment_attempts (
 id VARCHAR(64) PRIMARY KEY, assessment_id VARCHAR(64) NOT NULL, attempt_no INT NOT NULL, started_at DATETIME(3) NOT NULL,
 finished_at DATETIME(3) NULL, lease_expires_at DATETIME(3) NOT NULL, lease_token VARCHAR(64) NOT NULL,
 error_code VARCHAR(100) NOT NULL DEFAULT '', retryable BOOLEAN NOT NULL DEFAULT FALSE, provider_request_id VARCHAR(200) NOT NULL DEFAULT '',
 UNIQUE KEY assessment_attempt_number (assessment_id, attempt_no),
 CONSTRAINT assessment_attempt_fk FOREIGN KEY (assessment_id) REFERENCES assessments(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE assessment_results (
 id VARCHAR(64) PRIMARY KEY, assessment_id VARCHAR(64) NOT NULL, attempt_id VARCHAR(64) NOT NULL,
 input_snapshot_id VARCHAR(64) NOT NULL, content_json JSON NOT NULL, generated_at DATETIME(3) NOT NULL,
 UNIQUE KEY assessment_effective_result (assessment_id), UNIQUE KEY assessment_attempt_result (attempt_id),
 CONSTRAINT assessment_result_fk FOREIGN KEY (assessment_id) REFERENCES assessments(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE assessment_reviews (
 id VARCHAR(64) PRIMARY KEY, assessment_id VARCHAR(64) NOT NULL, result_id VARCHAR(64) NOT NULL, version INT NOT NULL,
 decision VARCHAR(16) NOT NULL, note TEXT NOT NULL, final_json JSON NULL, reviewed_by_employee_id VARCHAR(64) NOT NULL,
 doctor_name VARCHAR(100) NOT NULL, reviewed_at DATETIME(3) NOT NULL, supersedes_review_id VARCHAR(64) NOT NULL DEFAULT '',
 UNIQUE KEY assessment_review_version (assessment_id, version),
 CONSTRAINT assessment_review_fk FOREIGN KEY (assessment_id) REFERENCES assessments(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE assessment_consent_events (
 id VARCHAR(64) PRIMARY KEY, assessment_id VARCHAR(64) NOT NULL, patient_id VARCHAR(64) NOT NULL,
 policy_version VARCHAR(64) NOT NULL, policy_content TEXT NOT NULL, granted BOOLEAN NOT NULL, created_at DATETIME(3) NOT NULL,
 CONSTRAINT assessment_consent_fk FOREIGN KEY (assessment_id) REFERENCES assessments(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE assessment_events (
 id VARCHAR(64) PRIMARY KEY, assessment_id VARCHAR(64) NOT NULL, action VARCHAR(100) NOT NULL, actor_id VARCHAR(64) NOT NULL,
 version INT NOT NULL, detail VARCHAR(500) NOT NULL DEFAULT '', created_at DATETIME(3) NOT NULL,
 KEY assessment_audit (assessment_id, version), CONSTRAINT assessment_event_fk FOREIGN KEY (assessment_id) REFERENCES assessments(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
CREATE TABLE assessment_operations (
 assessment_id VARCHAR(64) NOT NULL, request_key VARCHAR(64) NOT NULL, request_hash CHAR(64) NOT NULL,
 action VARCHAR(64) NOT NULL, response_json JSON NOT NULL, created_at DATETIME(3) NOT NULL,
 PRIMARY KEY (assessment_id, request_key), CONSTRAINT assessment_operation_fk FOREIGN KEY (assessment_id) REFERENCES assessments(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE assessment_operations, assessment_events, assessment_consent_events, assessment_reviews, assessment_results, assessment_attempts, assessment_input_snapshots, assessment_photos, assessments;
