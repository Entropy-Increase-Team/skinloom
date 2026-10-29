-- +goose Up
-- create index "user_created_at" to table: "users"
CREATE INDEX `user_created_at` ON `users` (`created_at`);
-- create index "texture_created_at" to table: "textures"
CREATE INDEX `texture_created_at` ON `textures` (`created_at`);
-- create "reports" table
CREATE TABLE `reports` (`id` bigint NOT NULL AUTO_INCREMENT, `reason` varchar(500) NOT NULL, `status` enum('pending','resolved','rejected') NOT NULL DEFAULT "pending", `action` enum('delete','ban','reject') NULL, `handled_at` datetime(6) NULL, `created_at` datetime(6) NOT NULL, `texture_id` bigint NULL, `reporter_id` bigint NULL, `uploader_id` bigint NULL, `handler_id` bigint NULL, PRIMARY KEY (`id`), INDEX `report_handler_id` (`handler_id`), UNIQUE INDEX `report_reporter_id_texture_id` (`reporter_id`, `texture_id`), INDEX `report_status_id` (`status`, `id`), INDEX `report_texture_id` (`texture_id`), INDEX `report_uploader_id` (`uploader_id`), CONSTRAINT `reports_textures_reports` FOREIGN KEY (`texture_id`) REFERENCES `textures` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT `reports_users_reports_against` FOREIGN KEY (`uploader_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT `reports_users_reports_filed` FOREIGN KEY (`reporter_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT `reports_users_reports_handled` FOREIGN KEY (`handler_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "reports" table
DROP TABLE `reports`;
-- reverse: create index "texture_created_at" to table: "textures"
DROP INDEX `texture_created_at` ON `textures`;
-- reverse: create index "user_created_at" to table: "users"
DROP INDEX `user_created_at` ON `users`;
