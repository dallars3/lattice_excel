CREATE TABLE IF NOT EXISTS `excel_calc_jobs` (
  `job_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `table_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `status` VARCHAR(16) NOT NULL,
  `payload` JSON NOT NULL,
  `attempts` INT UNSIGNED NOT NULL DEFAULT 1,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`job_id`),
  KEY `idx_excel_calc_jobs_table_status` (`table_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `excel_calc_materialized` (
  `record_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `field_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `value_json` JSON NULL,
  `formula_hash` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
  `status` VARCHAR(16) NOT NULL,
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`record_id`, `field_id`),
  KEY `idx_excel_calc_materialized_status` (`status`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
