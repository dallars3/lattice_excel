CREATE TABLE IF NOT EXISTS `excel_records_oplog` (
  `client_request_id` VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `kind` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `request_json` JSON NOT NULL,
  `response_json` JSON NOT NULL,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`client_request_id`),
  KEY `idx_excel_records_oplog_kind_created` (`kind`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
