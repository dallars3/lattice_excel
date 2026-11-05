-- 000018 down: 移除公式错误原因列。
ALTER TABLE `excel_calc_materialized`
  DROP COLUMN `reason`;