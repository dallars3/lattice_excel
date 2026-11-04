CREATE TABLE IF NOT EXISTS `excel_scheduler_tasks` (
  `id` CHAR(26) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  `type` VARCHAR(64) NOT NULL,
  `status` ENUM('pending', 'running', 'success', 'failed', 'dead') NOT NULL DEFAULT 'pending',
  `run_at` BIGINT NOT NULL,
  `retry_count` INT UNSIGNED NOT NULL DEFAULT 0,
  `max_retries` INT UNSIGNED NOT NULL DEFAULT 5,
  `lease_until` BIGINT NULL,
  `owner_pid` VARCHAR(128) NULL,
  `payload` JSON NOT NULL,
  `last_error` VARCHAR(1024) NULL,
  `created_at` BIGINT NOT NULL,
  `updated_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_excel_scheduler_tasks_due` (`status`, `run_at`, `lease_until`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
