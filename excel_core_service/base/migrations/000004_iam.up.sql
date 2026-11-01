CREATE TABLE IF NOT EXISTS `excel_iam_users` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `username` VARCHAR(20) NOT NULL,
  `display_name` VARCHAR(20) NOT NULL,
  `avatar_uri` VARCHAR(512) NOT NULL DEFAULT '',
  `password_hash` VARCHAR(100) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `enterprise_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `internal` TINYINT(1) NOT NULL DEFAULT 0,
  `link_base_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `link_token` VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `link_role` VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_excel_iam_users_username` (`username`),
  KEY `idx_excel_iam_users_enterprise_id` (`enterprise_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `excel_iam_enterprises` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `name` VARCHAR(30) NOT NULL,
  `owner_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `member_count` INT NOT NULL DEFAULT 1,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_excel_iam_enterprises_owner_id` (`owner_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `excel_iam_members` (
  `enterprise_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `role` VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `status` VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `joined_at` BIGINT NOT NULL,
  PRIMARY KEY (`enterprise_id`, `user_id`),
  UNIQUE KEY `uk_excel_iam_members_user_id` (`user_id`),
  KEY `idx_excel_iam_members_enterprise_role` (`enterprise_id`, `role`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
