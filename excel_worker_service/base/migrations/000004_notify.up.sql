CREATE TABLE IF NOT EXISTS `excel_notify_notifications` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `user_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `type` VARCHAR(64) NOT NULL,
  `base_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `record_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `comment_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `payload_map` JSON NOT NULL,
  `read` TINYINT(1) NOT NULL DEFAULT 0,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_excel_notify_notifications_user_created` (`user_id`, `created_at`),
  KEY `idx_excel_notify_notifications_user_unread` (`user_id`, `read`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
