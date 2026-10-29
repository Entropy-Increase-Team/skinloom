-- +goose Up
-- create index "user_created_at" to table: "users"
CREATE INDEX `user_created_at` ON `users` (`created_at`);
-- create index "texture_created_at" to table: "textures"
CREATE INDEX `texture_created_at` ON `textures` (`created_at`);
-- create "reports" table
CREATE TABLE `reports` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `reason` text NOT NULL, `status` text NOT NULL DEFAULT ('pending'), `action` text NULL, `handled_at` datetime NULL, `created_at` datetime NOT NULL, `texture_id` integer NULL, `reporter_id` integer NULL, `uploader_id` integer NULL, `handler_id` integer NULL, CONSTRAINT `reports_textures_reports` FOREIGN KEY (`texture_id`) REFERENCES `textures` (`id`) ON DELETE SET NULL, CONSTRAINT `reports_users_reports_filed` FOREIGN KEY (`reporter_id`) REFERENCES `users` (`id`) ON DELETE SET NULL, CONSTRAINT `reports_users_reports_against` FOREIGN KEY (`uploader_id`) REFERENCES `users` (`id`) ON DELETE SET NULL, CONSTRAINT `reports_users_reports_handled` FOREIGN KEY (`handler_id`) REFERENCES `users` (`id`) ON DELETE SET NULL);
-- create index "report_reporter_id_texture_id" to table: "reports"
CREATE UNIQUE INDEX `report_reporter_id_texture_id` ON `reports` (`reporter_id`, `texture_id`);
-- create index "report_texture_id" to table: "reports"
CREATE INDEX `report_texture_id` ON `reports` (`texture_id`);
-- create index "report_uploader_id" to table: "reports"
CREATE INDEX `report_uploader_id` ON `reports` (`uploader_id`);
-- create index "report_handler_id" to table: "reports"
CREATE INDEX `report_handler_id` ON `reports` (`handler_id`);
-- create index "report_status_id" to table: "reports"
CREATE INDEX `report_status_id` ON `reports` (`status`, `id`);

-- +goose Down
-- reverse: create index "report_status_id" to table: "reports"
DROP INDEX `report_status_id`;
-- reverse: create index "report_handler_id" to table: "reports"
DROP INDEX `report_handler_id`;
-- reverse: create index "report_uploader_id" to table: "reports"
DROP INDEX `report_uploader_id`;
-- reverse: create index "report_texture_id" to table: "reports"
DROP INDEX `report_texture_id`;
-- reverse: create index "report_reporter_id_texture_id" to table: "reports"
DROP INDEX `report_reporter_id_texture_id`;
-- reverse: create "reports" table
DROP TABLE `reports`;
-- reverse: create index "texture_created_at" to table: "textures"
DROP INDEX `texture_created_at`;
-- reverse: create index "user_created_at" to table: "users"
DROP INDEX `user_created_at`;
