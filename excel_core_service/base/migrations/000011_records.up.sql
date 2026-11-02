CREATE TABLE IF NOT EXISTS `excel_records` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `table_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `cells_json` JSON NOT NULL,
  `created_by` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` BIGINT NOT NULL,
  `version` BIGINT NOT NULL DEFAULT 1,
  `soft_deleted_at` BIGINT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_excel_records_table_created` (`table_id`, `created_at`, `id`),
  KEY `idx_excel_records_table_creator_created` (`table_id`, `created_by`, `created_at`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `excel_records_histories` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `record_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `field_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `before` JSON NULL,
  `after` JSON NULL,
  `operator` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` BIGINT NOT NULL,
  `seq` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_excel_records_histories_seq` (`seq`),
  KEY `idx_excel_records_histories_record_time` (`record_id`, `created_at`, `seq`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
