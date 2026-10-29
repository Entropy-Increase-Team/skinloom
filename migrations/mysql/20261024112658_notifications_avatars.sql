-- +goose Up
-- modify "users" table
ALTER TABLE `users` ADD COLUMN `avatar_texture_id` bigint NULL, ADD COLUMN `announcements_seen` bigint NOT NULL DEFAULT 0, ADD INDEX `user_avatar_texture_id` (`avatar_texture_id`);
-- create "announcements" table
CREATE TABLE `announcements` (`id` bigint NOT NULL AUTO_INCREMENT, `title` varchar(64) NOT NULL, `content` longtext NOT NULL, `audience` enum('all','users') NOT NULL DEFAULT "all", `created_at` datetime(6) NOT NULL, `sender_id` bigint NULL, PRIMARY KEY (`id`), INDEX `announcement_sender_id` (`sender_id`), CONSTRAINT `announcements_users_announcements_sent` FOREIGN KEY (`sender_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "notifications" table
CREATE TABLE `notifications` (`id` bigint NOT NULL AUTO_INCREMENT, `title` varchar(64) NOT NULL, `content` longtext NOT NULL, `read_at` datetime(6) NULL, `created_at` datetime(6) NOT NULL, `announcement_id` bigint NULL, `user_id` bigint NOT NULL, PRIMARY KEY (`id`), INDEX `notification_announcement_id` (`announcement_id`), UNIQUE INDEX `notification_user_id_announcement_id` (`user_id`, `announcement_id`), INDEX `notification_user_id_id` (`user_id`, `id`), CONSTRAINT `notifications_announcements_notifications` FOREIGN KEY (`announcement_id`) REFERENCES `announcements` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT `notifications_users_notifications` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "notifications" table
DROP TABLE `notifications`;
-- reverse: create "announcements" table
DROP TABLE `announcements`;
-- reverse: modify "users" table
ALTER TABLE `users` DROP INDEX `user_avatar_texture_id`, DROP COLUMN `announcements_seen`, DROP COLUMN `avatar_texture_id`;
