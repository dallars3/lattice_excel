-- 票 03（N7 收尾）邀请语义修复：
-- 1) excel_iam_invitations 增加 role 列——接受邀请按「邀请时授予的角色」在目标企业
--    建成员行（当前 API 固定授予 member；列默认 member，存量行自动回填）。
-- 2) 删除 excel_iam_members 的单企业归属唯一键 uk_excel_iam_members_user_id——接受
--    邀请不再改写用户企业字段，同一账号可在多家企业持有成员行；主键
--    (enterprise_id, user_id) 已保证「同一企业一人一行」。
SET @excel_iam_add_invitation_role := IF(
  (SELECT COUNT(*) FROM information_schema.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_invitations'
     AND COLUMN_NAME = 'role') = 0,
  'ALTER TABLE `excel_iam_invitations` ADD COLUMN `role` VARCHAR(20) NOT NULL DEFAULT ''member'' AFTER `user_id`',
  'SELECT 1'
);
PREPARE excel_iam_invite_semantics_stmt FROM @excel_iam_add_invitation_role;
EXECUTE excel_iam_invite_semantics_stmt;
DEALLOCATE PREPARE excel_iam_invite_semantics_stmt;

SET @excel_iam_drop_member_user_unique := IF(
  (SELECT COUNT(*) FROM information_schema.STATISTICS
   WHERE TABLE_SCHEMA = DATABASE()
     AND TABLE_NAME = 'excel_iam_members'
     AND INDEX_NAME = 'uk_excel_iam_members_user_id') > 0,
  'ALTER TABLE `excel_iam_members` DROP INDEX `uk_excel_iam_members_user_id`',
  'SELECT 1'
);
PREPARE excel_iam_invite_semantics_stmt FROM @excel_iam_drop_member_user_unique;
EXECUTE excel_iam_invite_semantics_stmt;
DEALLOCATE PREPARE excel_iam_invite_semantics_stmt;
