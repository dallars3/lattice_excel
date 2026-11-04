-- 000016: 字段描述——字段对象增 description 列（≤500 字符，见 PRD FR-03-2）。
ALTER TABLE `excel_structure_fields`
  ADD COLUMN `description` VARCHAR(500) NOT NULL DEFAULT '' AFTER `config`;