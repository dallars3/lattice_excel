CREATE TABLE IF NOT EXISTS `excel_comment_comments` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `record_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `parent_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `content` VARCHAR(2000) NOT NULL,
  `author` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `status` VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `mentions` JSON NOT NULL,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_excel_comment_comments_record_created` (`record_id`, `created_at`, `id`),
  KEY `idx_excel_comment_comments_parent` (`parent_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
