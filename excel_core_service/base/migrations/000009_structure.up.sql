CREATE TABLE IF NOT EXISTS `excel_structure_tables` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `enterprise_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `base_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `name` VARCHAR(30) NOT NULL,
  `position` INT NOT NULL DEFAULT 0,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_excel_structure_tables_base_name` (`base_id`, `name`),
  KEY `idx_excel_structure_tables_enterprise` (`enterprise_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `excel_structure_fields` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `table_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `name` VARCHAR(30) NOT NULL,
  `type` VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `config` JSON NOT NULL,
  `is_primary` TINYINT(1) NOT NULL DEFAULT 0,
  `position` INT NOT NULL DEFAULT 0,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_excel_structure_fields_table_name` (`table_id`, `name`),
  KEY `idx_excel_structure_fields_table_position` (`table_id`, `position`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `excel_structure_views` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `table_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `name` VARCHAR(30) NOT NULL,
  `type` VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'table',
  `config` JSON NOT NULL,
  `position` INT NOT NULL DEFAULT 0,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_excel_structure_views_table_name` (`table_id`, `name`),
  KEY `idx_excel_structure_views_table_position` (`table_id`, `position`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS `excel_structure_field_options` (
  `field_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `option_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `name` VARCHAR(30) NOT NULL,
  `color` VARCHAR(32) NOT NULL DEFAULT '',
  `position` INT NOT NULL DEFAULT 0,
  PRIMARY KEY (`field_id`, `option_id`),
  KEY `idx_excel_structure_field_options_name` (`field_id`, `name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
