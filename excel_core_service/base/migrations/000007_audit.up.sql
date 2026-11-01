CREATE TABLE IF NOT EXISTS `excel_audit_events` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `event_id` VARCHAR(64) NOT NULL,
  `enterprise_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `action` VARCHAR(64) NOT NULL,
  `operator` VARCHAR(128) NOT NULL,
  `target` VARCHAR(256) NOT NULL,
  `base_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `table_id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NULL,
  `detail` JSON NULL,
  `occurred_at` DATETIME(3) NOT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY `uk_excel_audit_events_event_id` (`event_id`),
  KEY `idx_excel_audit_events_enterprise_time` (`enterprise_id`, `occurred_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
