-- 回滚票 03：恢复单企业归属唯一键并移除邀请角色列。若存量数据已有同账号多企业
-- 成员行，恢复唯一键将失败（属预期：多成员行数据与新约束不兼容，不可自动回滚）。
SET @excel_iam_add_member_user_unique := IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_members'
     AND INDEX_NAME = 'uk_excel_iam_members_user_id') = 0,
  'ALTER TABLE `excel_iam_members` ADD UNIQUE KEY `uk_excel_iam_members_user_id` (`user_id`)',
  'SELECT 1'
);
PREPARE excel_iam_invite_semantics_down_stmt FROM @excel_iam_add_member_user_unique;
EXECUTE excel_iam_invite_semantics_down_stmt;
DEALLOCATE PREPARE excel_iam_invite_semantics_down_stmt;

SET @excel_iam_drop_invitation_role := IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_invitations'
     AND COLUMN_NAME = 'role') > 0,
  'ALTER TABLE `excel_iam_invitations` DROP COLUMN `role`',
  'SELECT 1'
);
PREPARE excel_iam_invite_semantics_down_stmt FROM @excel_iam_drop_invitation_role;
EXECUTE excel_iam_invite_semantics_down_stmt;
DEALLOCATE PREPARE excel_iam_invite_semantics_down_stmt;
