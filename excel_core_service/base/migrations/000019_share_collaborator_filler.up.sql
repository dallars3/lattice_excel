-- 000019: 协作者可填写——excel_share_collaborators.role 枚举补 filler（H-64 授权可填写，外部上限仍为 editor）。
ALTER TABLE `excel_share_collaborators`
  MODIFY COLUMN `role` ENUM('viewer', 'filler', 'editor', 'manager') NOT NULL;
