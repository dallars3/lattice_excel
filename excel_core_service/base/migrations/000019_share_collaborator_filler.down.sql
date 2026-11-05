-- 000019 down: 回收 filler 枚举前先降级存量可填写协作者为可编辑。
UPDATE `excel_share_collaborators` SET `role` = 'editor' WHERE `role` = 'filler';
ALTER TABLE `excel_share_collaborators`
  MODIFY COLUMN `role` ENUM('viewer', 'editor', 'manager') NOT NULL;
