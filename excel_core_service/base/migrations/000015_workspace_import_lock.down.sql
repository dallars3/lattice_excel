-- 000015 down: 移除导入锁列。
ALTER TABLE `excel_workspace_bases`
  DROP COLUMN `import_status`;
