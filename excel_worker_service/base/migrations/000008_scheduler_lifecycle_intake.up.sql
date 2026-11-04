ALTER TABLE `excel_scheduler_tasks`
  ADD COLUMN `kind` VARCHAR(64) NULL AFTER `type`,
  ADD COLUMN `entity_id` VARCHAR(64) NULL AFTER `kind`,
  ADD COLUMN `event_id` VARCHAR(64) NULL AFTER `entity_id`,
  ADD UNIQUE KEY `uniq_excel_scheduler_tasks_kind_entity_status` (`kind`, `entity_id`, `status`);