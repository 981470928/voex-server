CREATE TABLE IF NOT EXISTS ai_conversation_sessions (
  id bigint unsigned NOT NULL AUTO_INCREMENT,
  user_id char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  document_id int NOT NULL,
  session_id varchar(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  title varchar(80) NOT NULL,
  created_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_ai_session (user_id, document_id, session_id),
  KEY idx_ai_sessions (user_id, document_id, id),
  CONSTRAINT fk_ai_session_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_ai_session_document FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
