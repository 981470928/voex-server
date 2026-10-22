CREATE TABLE IF NOT EXISTS ai_models (
  id varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  user_id char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  name varchar(128) NOT NULL,
  protocol varchar(16) NOT NULL,
  base_url varchar(2048) NOT NULL,
  model varchar(255) NOT NULL,
  key_cipher text NOT NULL,
  temperature double DEFAULT NULL,
  max_tokens int unsigned NOT NULL DEFAULT 4096,
  token_parameter varchar(24) NOT NULL DEFAULT 'max_completion_tokens',
  created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id), KEY idx_ai_model_user (user_id),
  CONSTRAINT fk_ai_model_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS ai_review_applications (
  review_id varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  user_id char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  payload_hash char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  result_json json NOT NULL,
  created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id, review_id),
  CONSTRAINT fk_ai_review_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
