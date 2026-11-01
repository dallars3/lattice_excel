CREATE TABLE IF NOT EXISTS `excel_iam_invitations` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `enterprise_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `username` VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `status` VARCHAR(20) NOT NULL,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_excel_iam_invitations_enterprise_username` (`enterprise_id`, `username`),
  KEY `idx_excel_iam_invitations_user_id` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
