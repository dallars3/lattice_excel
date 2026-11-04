-- 000018: 公式错误原因——物化表增 reason 列（错误值编码 #ERROR:<原因> 的原文透传）。
ALTER TABLE `excel_calc_materialized`
  ADD COLUMN `reason` VARCHAR(500) NOT NULL DEFAULT '' AFTER `status`;