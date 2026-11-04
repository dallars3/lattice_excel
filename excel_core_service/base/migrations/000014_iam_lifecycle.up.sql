CREATE TABLE IF NOT EXISTS `excel_iam_register_switch` (
  `id` TINYINT UNSIGNED NOT NULL PRIMARY KEY,
  `enabled` TINYINT(1) NOT NULL DEFAULT 1,
  `updated_at` BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO `excel_iam_register_switch` (`id`, `enabled`, `updated_at`)
VALUES (1, 1, UNIX_TIMESTAMP() * 1000)
ON DUPLICATE KEY UPDATE `id` = VALUES(`id`);

SET @excel_iam_add_dissolution_status := IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_enterprises'
     AND COLUMN_NAME = 'dissolution_status') = 0,
  'ALTER TABLE `excel_iam_enterprises` ADD COLUMN `dissolution_status` VARCHAR(20) NOT NULL DEFAULT ''none'' AFTER `member_count`',
  'SELECT 1'
);
PREPARE excel_iam_lifecycle_stmt FROM @excel_iam_add_dissolution_status;
EXECUTE excel_iam_lifecycle_stmt;
DEALLOCATE PREPARE excel_iam_lifecycle_stmt;

SET @excel_iam_add_dissolution_deadline := IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_enterprises'
     AND COLUMN_NAME = 'dissolution_deadline') = 0,
  'ALTER TABLE `excel_iam_enterprises` ADD COLUMN `dissolution_deadline` BIGINT NULL AFTER `dissolution_status`',
  'SELECT 1'
);
PREPARE excel_iam_lifecycle_stmt FROM @excel_iam_add_dissolution_deadline;
EXECUTE excel_iam_lifecycle_stmt;
DEALLOCATE PREPARE excel_iam_lifecycle_stmt;

SET @excel_iam_add_user_status := IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_users'
     AND COLUMN_NAME = 'status') = 0,
  'ALTER TABLE `excel_iam_users` ADD COLUMN `status` VARCHAR(20) NOT NULL DEFAULT ''active'' AFTER `internal`',
  'SELECT 1'
);
PREPARE excel_iam_lifecycle_stmt FROM @excel_iam_add_user_status;
EXECUTE excel_iam_lifecycle_stmt;
DEALLOCATE PREPARE excel_iam_lifecycle_stmt;

SET @excel_iam_add_deactivation_deadline := IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_users'
     AND COLUMN_NAME = 'deactivation_deadline') = 0,
  'ALTER TABLE `excel_iam_users` ADD COLUMN `deactivation_deadline` BIGINT NULL AFTER `status`',
  'SELECT 1'
);
PREPARE excel_iam_lifecycle_stmt FROM @excel_iam_add_deactivation_deadline;
EXECUTE excel_iam_lifecycle_stmt;
DEALLOCATE PREPARE excel_iam_lifecycle_stmt;
