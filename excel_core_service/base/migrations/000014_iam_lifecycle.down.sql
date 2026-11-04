SET @excel_iam_drop_deactivation_deadline := IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_users'
     AND COLUMN_NAME = 'deactivation_deadline') > 0,
  'ALTER TABLE `excel_iam_users` DROP COLUMN `deactivation_deadline`',
  'SELECT 1'
);
PREPARE excel_iam_lifecycle_stmt FROM @excel_iam_drop_deactivation_deadline;
EXECUTE excel_iam_lifecycle_stmt;
DEALLOCATE PREPARE excel_iam_lifecycle_stmt;

SET @excel_iam_drop_user_status := IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_users'
     AND COLUMN_NAME = 'status') > 0,
  'ALTER TABLE `excel_iam_users` DROP COLUMN `status`',
  'SELECT 1'
);
PREPARE excel_iam_lifecycle_stmt FROM @excel_iam_drop_user_status;
EXECUTE excel_iam_lifecycle_stmt;
DEALLOCATE PREPARE excel_iam_lifecycle_stmt;

SET @excel_iam_drop_dissolution_deadline := IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_enterprises'
     AND COLUMN_NAME = 'dissolution_deadline') > 0,
  'ALTER TABLE `excel_iam_enterprises` DROP COLUMN `dissolution_deadline`',
  'SELECT 1'
);
PREPARE excel_iam_lifecycle_stmt FROM @excel_iam_drop_dissolution_deadline;
EXECUTE excel_iam_lifecycle_stmt;
DEALLOCATE PREPARE excel_iam_lifecycle_stmt;

SET @excel_iam_drop_dissolution_status := IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_enterprises'
     AND COLUMN_NAME = 'dissolution_status') > 0,
  'ALTER TABLE `excel_iam_enterprises` DROP COLUMN `dissolution_status`',
  'SELECT 1'
);
PREPARE excel_iam_lifecycle_stmt FROM @excel_iam_drop_dissolution_status;
EXECUTE excel_iam_lifecycle_stmt;
DEALLOCATE PREPARE excel_iam_lifecycle_stmt;

DROP TABLE IF EXISTS `excel_iam_register_switch`;
