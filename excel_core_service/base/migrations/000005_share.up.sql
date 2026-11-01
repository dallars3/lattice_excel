CREATE TABLE IF NOT EXISTS `excel_share_collaborators` (
  `base_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `role` ENUM('viewer', 'editor', 'manager') NOT NULL,
  `internal` TINYINT(1) NOT NULL DEFAULT 0,
  `source` ENUM('invite', 'link') NOT NULL,
  `granted_at` BIGINT NOT NULL,
  PRIMARY KEY (`base_id`, `user_id`),
  KEY `idx_excel_share_collaborators_role` (`base_id`, `role`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `excel_share_links` (
  `base_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `token` VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `scope` ENUM('none', 'enterprise', 'anyone') NOT NULL,
  `role` ENUM('viewer', 'filler') NOT NULL,
  `enabled` TINYINT(1) NOT NULL DEFAULT 0,
  `url` VARCHAR(512) NOT NULL DEFAULT '',
  `updated_at` BIGINT NOT NULL,
  PRIMARY KEY (`base_id`),
  UNIQUE KEY `uk_excel_share_links_token` (`token`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
