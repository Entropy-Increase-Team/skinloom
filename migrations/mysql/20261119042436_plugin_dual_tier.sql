-- +goose Up
-- modify "plugins" table
ALTER TABLE `plugins` ADD COLUMN `runtime` enum('js','native') NOT NULL DEFAULT "js", ADD COLUMN `platform_packages` json NULL, ADD COLUMN `registry_source` varchar(512) NULL DEFAULT "";

-- +goose Down
-- reverse: modify "plugins" table
ALTER TABLE `plugins` DROP COLUMN `registry_source`, DROP COLUMN `platform_packages`, DROP COLUMN `runtime`;
