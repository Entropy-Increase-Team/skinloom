-- +goose Up
-- add columns to "users" (ADD COLUMN, not atlas's table rebuild: goose runs each file in a
-- transaction with foreign keys on, where DROP TABLE `users` would cascade into every child table)
ALTER TABLE `users` ADD COLUMN `avatar_texture_id` integer NULL;
ALTER TABLE `users` ADD COLUMN `announcements_seen` integer NOT NULL DEFAULT (0);
-- create index "user_avatar_texture_id" to table: "users"
CREATE INDEX `user_avatar_texture_id` ON `users` (`avatar_texture_id`);
-- create "announcements" table
CREATE TABLE `announcements` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `title` text NOT NULL, `content` text NOT NULL, `audience` text NOT NULL DEFAULT ('all'), `created_at` datetime NOT NULL, `sender_id` integer NULL, CONSTRAINT `announcements_users_announcements_sent` FOREIGN KEY (`sender_id`) REFERENCES `users` (`id`) ON DELETE SET NULL);
-- create index "announcement_sender_id" to table: "announcements"
CREATE INDEX `announcement_sender_id` ON `announcements` (`sender_id`);
-- create "notifications" table
CREATE TABLE `notifications` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `title` text NOT NULL, `content` text NOT NULL, `read_at` datetime NULL, `created_at` datetime NOT NULL, `announcement_id` integer NULL, `user_id` integer NOT NULL, CONSTRAINT `notifications_announcements_notifications` FOREIGN KEY (`announcement_id`) REFERENCES `announcements` (`id`) ON DELETE SET NULL, CONSTRAINT `notifications_users_notifications` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "notification_user_id_id" to table: "notifications"
CREATE INDEX `notification_user_id_id` ON `notifications` (`user_id`, `id`);
-- create index "notification_user_id_announcement_id" to table: "notifications"
CREATE UNIQUE INDEX `notification_user_id_announcement_id` ON `notifications` (`user_id`, `announcement_id`);
-- create index "notification_announcement_id" to table: "notifications"
CREATE INDEX `notification_announcement_id` ON `notifications` (`announcement_id`);

-- +goose Down
-- reverse: create index "notification_announcement_id" to table: "notifications"
DROP INDEX `notification_announcement_id`;
-- reverse: create index "notification_user_id_announcement_id" to table: "notifications"
DROP INDEX `notification_user_id_announcement_id`;
-- reverse: create index "notification_user_id_id" to table: "notifications"
DROP INDEX `notification_user_id_id`;
-- reverse: create "notifications" table
DROP TABLE `notifications`;
-- reverse: create index "announcement_sender_id" to table: "announcements"
DROP INDEX `announcement_sender_id`;
-- reverse: create "announcements" table
DROP TABLE `announcements`;
-- reverse: add columns to "users"
DROP INDEX `user_avatar_texture_id`;
ALTER TABLE `users` DROP COLUMN `announcements_seen`;
ALTER TABLE `users` DROP COLUMN `avatar_texture_id`;
