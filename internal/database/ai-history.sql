CREATE TABLE IF NOT EXISTS ai_conversation_turns (
  id bigint unsigned NOT NULL AUTO_INCREMENT,
  user_id char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  document_id int NOT NULL,
  turn_id varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  model_id varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  prompt longtext NOT NULL,
  response longtext NOT NULL,
  activity json NOT NULL,
  error text NOT NULL,
  review_id varchar(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  review_json json DEFAULT NULL,
  finish_hash char(64) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  created_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  completed_at datetime(3) DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_ai_turn (user_id, turn_id),
  UNIQUE KEY uq_ai_turn_review (user_id, review_id),
  KEY idx_ai_conversation (user_id, document_id, id),
  CONSTRAINT fk_ai_turn_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_ai_turn_document FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO schema_migrations (version) VALUES ('008-ai-conversation-history');
