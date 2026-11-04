CREATE TABLE IF NOT EXISTS `excel_tracking_events` (
  `id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  `event_id` VARCHAR(64) NOT NULL,
  `user_id` VARCHAR(64) NOT NULL,
  `user_type` VARCHAR(16) NOT NULL,
  `event_name` VARCHAR(64) NOT NULL,
  `detail` JSON NULL,
  `client_at_ms` BIGINT NULL,
  `received_at_ms` BIGINT NOT NULL,
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY `uk_excel_tracking_events_event_id` (`event_id`),
  KEY `idx_excel_tracking_events_received_at` (`received_at_ms`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;