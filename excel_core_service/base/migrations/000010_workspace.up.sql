CREATE TABLE IF NOT EXISTS `excel_workspace_bases` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `enterprise_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `owner_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `name` VARCHAR(50) NOT NULL,
  `last_modified_at` BIGINT NOT NULL,
  `last_modifier` VARCHAR(128) NOT NULL,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_excel_workspace_bases_enterprise` (`enterprise_id`),
  KEY `idx_excel_workspace_bases_owner` (`owner_id`, `last_modified_at`),
  KEY `idx_excel_workspace_bases_enterprise_name` (`enterprise_id`, `name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `excel_workspace_stars` (
  `base_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`base_id`, `user_id`),
  KEY `idx_excel_workspace_stars_user` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `excel_workspace_recent` (
  `user_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `base_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `last_accessed_at` BIGINT NOT NULL,
  PRIMARY KEY (`user_id`, `base_id`),
  KEY `idx_excel_workspace_recent_user_time` (`user_id`, `last_accessed_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
