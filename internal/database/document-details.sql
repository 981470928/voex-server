ALTER TABLE `documents`
  ADD COLUMN `updated_by` char(36) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL AFTER `creator_id`,
  ADD COLUMN `updated_by_anonymous` tinyint(1) NOT NULL DEFAULT '0' AFTER `updated_by`,
  ADD KEY `idx_documents_updated_by` (`updated_by`),
  ADD CONSTRAINT `fk_documents_updated_by` FOREIGN KEY (`updated_by`) REFERENCES `users` (`id`);

INSERT IGNORE INTO schema_migrations (version) VALUES ('005-document-details');
