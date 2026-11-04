-- 000015: 导入锁——导入中的新 Base 全员不可访问（ticket 16）。
ALTER TABLE `excel_workspace_bases`
  ADD COLUMN `import_status` VARCHAR(16) NOT NULL DEFAULT '' AFTER `created_at`;
