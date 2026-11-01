CREATE TABLE IF NOT EXISTS `excel_bus_consumer_events` (
  `event_id` VARCHAR(64) NOT NULL PRIMARY KEY,
  `consumer_name` VARCHAR(128) NOT NULL,
  `status` ENUM('processing', 'processed', 'failed', 'dead') NOT NULL DEFAULT 'processing',
  `attempts` INT UNSIGNED NOT NULL DEFAULT 0,
  `last_error` VARCHAR(1024) NULL,
  `processed_at` DATETIME(3) NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  KEY `idx_excel_bus_consumer_events_status` (`status`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
