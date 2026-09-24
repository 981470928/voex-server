CREATE TABLE IF NOT EXISTS `assets` (
  `id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `creator_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `directory` varchar(255) NOT NULL,
  `storage_name` varchar(64) NOT NULL,
  `name` varchar(255) NOT NULL,
  `mime` varchar(128) NOT NULL,
  `size` bigint unsigned NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_asset_creator` (`creator_id`),
  CONSTRAINT `assets_ibfk_1` FOREIGN KEY (`creator_id`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `auth_sessions` (
  `id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `refresh_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `expires_at` datetime NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `refresh_hash` (`refresh_hash`),
  KEY `idx_session_expiry` (`expires_at`),
  KEY `user_id` (`user_id`),
  CONSTRAINT `auth_sessions_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `documents` (
  `id` int NOT NULL AUTO_INCREMENT,
  `file_key` varchar(64) NOT NULL,
  `file_name` varchar(255) NOT NULL DEFAULT 'untitled.md',
  `file_content` longtext,
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `folder_id` int NOT NULL,
  `creator_id` char(36) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `privileges` json NOT NULL,
  `revision` int unsigned NOT NULL DEFAULT '1',
  PRIMARY KEY (`id`),
  UNIQUE KEY `file_key` (`file_key`),
  KEY `idx_file_key` (`file_key`),
  KEY `idx_documents_folder` (`folder_id`,`created_at`,`id`),
  KEY `idx_documents_creator` (`creator_id`),
  CONSTRAINT `fk_documents_creator` FOREIGN KEY (`creator_id`) REFERENCES `users` (`id`),
  CONSTRAINT `fk_documents_folder` FOREIGN KEY (`folder_id`) REFERENCES `folders` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `file_shares` (
  `id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `document_id` int NOT NULL,
  `token_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `permission` enum('read','edit') NOT NULL,
  `created_by` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `revoked_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `token_hash` (`token_hash`),
  KEY `document_id` (`document_id`),
  KEY `created_by` (`created_by`),
  CONSTRAINT `file_shares_ibfk_1` FOREIGN KEY (`document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `file_shares_ibfk_2` FOREIGN KEY (`created_by`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `files` (
  `id` int NOT NULL AUTO_INCREMENT,
  `file_key` varchar(64) NOT NULL,
  `hash` varchar(128) NOT NULL,
  `name` varchar(255) NOT NULL,
  `mime` varchar(128) NOT NULL,
  `size` bigint NOT NULL DEFAULT '0',
  `created_at` datetime DEFAULT CURRENT_TIMESTAMP,
  `creator_id` char(36) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `privileges` json NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_file_key` (`file_key`),
  KEY `idx_hash` (`hash`),
  KEY `idx_files_creator` (`creator_id`),
  CONSTRAINT `fk_files_creator` FOREIGN KEY (`creator_id`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `folders` (
  `id` int NOT NULL AUTO_INCREMENT,
  `folder_key` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `project_id` int NOT NULL,
  `parent_id` int DEFAULT NULL,
  `name` varchar(255) NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `privileges` json NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `folder_key` (`folder_key`),
  UNIQUE KEY `uq_folders_project_id` (`project_id`,`id`),
  KEY `idx_folders_parent` (`project_id`,`parent_id`,`created_at`,`id`),
  KEY `idx_folders_order` (`project_id`,`created_at`,`id`),
  CONSTRAINT `fk_folders_parent` FOREIGN KEY (`project_id`, `parent_id`) REFERENCES `folders` (`project_id`, `id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
  CONSTRAINT `fk_folders_project` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE RESTRICT ON UPDATE RESTRICT,
  CONSTRAINT `chk_folders_flat` CHECK ((`parent_id` is null))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `project_members` (
  `project_id` int NOT NULL,
  `user_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  PRIMARY KEY (`project_id`,`user_id`),
  KEY `user_id` (`user_id`),
  CONSTRAINT `project_members_ibfk_1` FOREIGN KEY (`project_id`) REFERENCES `projects` (`id`) ON DELETE CASCADE,
  CONSTRAINT `project_members_ibfk_2` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `projects` (
  `id` int NOT NULL AUTO_INCREMENT,
  `project_key` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `name` varchar(255) NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `creator_id` char(36) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `team_id` int DEFAULT NULL,
  `privileges` json NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `project_key` (`project_key`),
  KEY `idx_projects_order` (`created_at`,`id`),
  KEY `idx_projects_creator` (`creator_id`),
  KEY `idx_projects_team` (`team_id`),
  CONSTRAINT `fk_projects_creator` FOREIGN KEY (`creator_id`) REFERENCES `users` (`id`),
  CONSTRAINT `fk_projects_team` FOREIGN KEY (`team_id`) REFERENCES `teams` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `schema_migrations` (
  `version` varchar(64) NOT NULL,
  `applied_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `team_invites` (
  `id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `team_id` int NOT NULL,
  `token_hash` char(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_by` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `revoked_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `token_hash` (`token_hash`),
  KEY `team_id` (`team_id`),
  KEY `created_by` (`created_by`),
  CONSTRAINT `team_invites_ibfk_1` FOREIGN KEY (`team_id`) REFERENCES `teams` (`id`) ON DELETE CASCADE,
  CONSTRAINT `team_invites_ibfk_2` FOREIGN KEY (`created_by`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `team_join_requests` (
  `id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `team_id` int NOT NULL,
  `user_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `message` varchar(1000) NOT NULL,
  `status` enum('pending','approved','rejected') NOT NULL DEFAULT 'pending',
  `reviewed_by` char(36) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `reviewed_at` datetime DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_team_request` (`team_id`,`user_id`),
  KEY `user_id` (`user_id`),
  KEY `reviewed_by` (`reviewed_by`),
  CONSTRAINT `team_join_requests_ibfk_1` FOREIGN KEY (`team_id`) REFERENCES `teams` (`id`) ON DELETE CASCADE,
  CONSTRAINT `team_join_requests_ibfk_2` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`),
  CONSTRAINT `team_join_requests_ibfk_3` FOREIGN KEY (`reviewed_by`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `team_members` (
  `team_id` int NOT NULL,
  `user_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `role` enum('admin','member') NOT NULL DEFAULT 'member',
  `joined_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`team_id`,`user_id`),
  KEY `user_id` (`user_id`),
  CONSTRAINT `team_members_ibfk_1` FOREIGN KEY (`team_id`) REFERENCES `teams` (`id`) ON DELETE CASCADE,
  CONSTRAINT `team_members_ibfk_2` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `teams` (
  `id` int NOT NULL AUTO_INCREMENT,
  `team_key` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `team_code` char(10) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `name` varchar(128) NOT NULL,
  `kind` enum('personal','standard') NOT NULL,
  `owner_id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `personal_owner_id` char(36) CHARACTER SET ascii COLLATE ascii_bin DEFAULT NULL,
  `privileges` json NOT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `team_key` (`team_key`),
  UNIQUE KEY `team_code` (`team_code`),
  UNIQUE KEY `personal_owner_id` (`personal_owner_id`),
  KEY `owner_id` (`owner_id`),
  CONSTRAINT `teams_ibfk_1` FOREIGN KEY (`owner_id`) REFERENCES `users` (`id`),
  CONSTRAINT `teams_ibfk_2` FOREIGN KEY (`personal_owner_id`) REFERENCES `users` (`id`),
  CONSTRAINT `teams_chk_1` CHECK ((((`kind` = _utf8mb4'personal') and (`personal_owner_id` is not null) and (`personal_owner_id` = `owner_id`)) or ((`kind` = _utf8mb4'standard') and (`personal_owner_id` is null))))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `users` (
  `id` char(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `account` varbinary(256) NOT NULL,
  `password_hash` varchar(255) NOT NULL,
  `name` varchar(64) NOT NULL,
  `avator` varchar(255) NOT NULL DEFAULT '',
  `email` varchar(254) NOT NULL DEFAULT '',
  `phone` varchar(32) NOT NULL DEFAULT '',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `account` (`account`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `workspace_state` (
  `id` tinyint NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT IGNORE INTO workspace_state (id) VALUES (1);

INSERT IGNORE INTO schema_migrations (version) VALUES ('001-workspace'),('002-auth'),('003-teams');
