CREATE TABLE IF NOT EXISTS `excel_file_meta` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `name` VARCHAR(255) NOT NULL,
  `display_name` VARCHAR(255) NOT NULL,
  `ext` VARCHAR(32) NOT NULL,
  `mime` VARCHAR(128) NOT NULL,
  `size` BIGINT NOT NULL,
  `usage` VARCHAR(32) NOT NULL,
  `uploader` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `upload_base` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `storage_path` VARCHAR(512) NOT NULL,
  `checksum` CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_excel_file_meta_uploader_created` (`uploader`, `created_at`),
  KEY `idx_excel_file_meta_usage` (`usage`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
