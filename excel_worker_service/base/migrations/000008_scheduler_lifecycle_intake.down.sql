ALTER TABLE `excel_scheduler_tasks`
  DROP INDEX `uniq_excel_scheduler_tasks_kind_entity_status`,
  DROP COLUMN `event_id`,
  DROP COLUMN `entity_id`,
  DROP COLUMN `kind`;