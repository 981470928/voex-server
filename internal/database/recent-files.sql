CREATE TABLE IF NOT EXISTS `recent_files` (
  `user_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `document_id` int NOT NULL,
  `share_id` char(36) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `file_name` varchar(255) NOT NULL,
  `viewed_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`user_id`,`document_id`),
  KEY `idx_recent_files_order` (`user_id`,`viewed_at`,`document_id`),
  KEY `idx_recent_files_document` (`document_id`),
  KEY `idx_recent_files_share` (`share_id`),
  CONSTRAINT `fk_recent_files_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recent_files_document` FOREIGN KEY (`document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recent_files_share` FOREIGN KEY (`share_id`) REFERENCES `file_shares` (`id`) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT IGNORE INTO schema_migrations (version) VALUES ('004-recent-files');
