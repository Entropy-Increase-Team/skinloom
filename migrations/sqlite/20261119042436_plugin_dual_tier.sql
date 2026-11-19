-- +goose Up
-- add columns to "plugins" (ADD COLUMN, not atlas's table rebuild: goose runs each file in a
-- transaction with foreign keys on, where DROP TABLE would violate TestSQLiteMigrationsDoNotRebuildTables)
ALTER TABLE `plugins` ADD COLUMN `runtime` text NOT NULL DEFAULT ('js');
ALTER TABLE `plugins` ADD COLUMN `platform_packages` json NULL;
ALTER TABLE `plugins` ADD COLUMN `registry_source` text NULL DEFAULT ('');

-- +goose Down
-- reverse: add columns to "plugins"
ALTER TABLE `plugins` DROP COLUMN `registry_source`;
ALTER TABLE `plugins` DROP COLUMN `platform_packages`;
ALTER TABLE `plugins` DROP COLUMN `runtime`;
