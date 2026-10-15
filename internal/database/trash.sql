ALTER TABLE `documents`
    ADD COLUMN `deleted_at` datetime(6) DEFAULT NULL,
  ADD COLUMN `deleted_by` char(36) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  ADD COLUMN `expires_at` datetime(6) DEFAULT NULL,
  ADD KEY `idx_documents_trash_expiry` (`expires_at`, `id`),
  ADD KEY `idx_documents_folder_deleted` (`folder_id`, `deleted_at`, `created_at`, `id`),
  ADD KEY `idx_documents_deleted_by` (`deleted_by`),
  ADD CONSTRAINT `fk_documents_deleted_by`
    FOREIGN KEY (`deleted_by`) REFERENCES `users` (`id`);

ALTER TABLE `folders`
    ADD COLUMN `deleted_at` datetime(6) DEFAULT NULL,
  ADD COLUMN `deleted_by` char(36) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  ADD COLUMN `expires_at` datetime(6) DEFAULT NULL,
  ADD KEY `idx_folders_trash_expiry` (`expires_at`, `id`),
  ADD KEY `idx_folders_project_deleted` (`project_id`, `deleted_at`, `created_at`, `id`),
  ADD KEY `idx_folders_deleted_by` (`deleted_by`),
  ADD CONSTRAINT `fk_folders_deleted_by`
    FOREIGN KEY (`deleted_by`) REFERENCES `users` (`id`);

INSERT IGNORE INTO `schema_migrations` (`version`)
VALUES ('006-trash');