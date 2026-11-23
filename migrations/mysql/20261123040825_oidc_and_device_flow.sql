-- +goose Up
-- modify "oauth_clients" table
ALTER TABLE `oauth_clients` ADD COLUMN `dynamic` bool NOT NULL DEFAULT 0, ADD COLUMN `last_used_at` datetime(6) NULL, ADD INDEX `oauthclient_dynamic_last_used_at` (`dynamic`, `last_used_at`);
-- modify "oauth_codes" table
ALTER TABLE `oauth_codes` ADD COLUMN `nonce` varchar(256) NOT NULL DEFAULT "", ADD COLUMN `player_id` bigint NULL, ADD COLUMN `auth_time` datetime(6) NULL, ADD INDEX `oauthcode_player_id` (`player_id`);
-- modify "oauth_tokens" table
ALTER TABLE `oauth_tokens` ADD COLUMN `player_id` bigint NULL, ADD COLUMN `player_name_at_issue` varchar(32) NOT NULL DEFAULT "", ADD INDEX `oauthtoken_player_id` (`player_id`);
-- create "oauth_device_codes" table
CREATE TABLE `oauth_device_codes` (`id` bigint NOT NULL AUTO_INCREMENT, `device_code_hash` varchar(64) NOT NULL, `user_code_hash` varchar(64) NOT NULL, `scopes` json NOT NULL, `status` enum('pending','approved','denied','consumed') NOT NULL DEFAULT "pending", `player_id` bigint NULL, `interval_sec` bigint NOT NULL DEFAULT 5, `last_polled_at` datetime(6) NULL, `expires_at` datetime(6) NOT NULL, `created_at` datetime(6) NOT NULL, `client_id` bigint NOT NULL, `user_id` bigint NULL, PRIMARY KEY (`id`), UNIQUE INDEX `device_code_hash` (`device_code_hash`), UNIQUE INDEX `user_code_hash` (`user_code_hash`), INDEX `oauthdevicecode_client_id` (`client_id`), INDEX `oauthdevicecode_user_id` (`user_id`), INDEX `oauthdevicecode_player_id` (`player_id`), INDEX `oauthdevicecode_expires_at` (`expires_at`), CONSTRAINT `oauth_device_codes_oauth_clients_device_codes` FOREIGN KEY (`client_id`) REFERENCES `oauth_clients` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT `oauth_device_codes_users_oauth_device_codes` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "oauth_device_codes" table
DROP TABLE `oauth_device_codes`;
-- reverse: modify "oauth_tokens" table
ALTER TABLE `oauth_tokens` DROP INDEX `oauthtoken_player_id`, DROP COLUMN `player_name_at_issue`, DROP COLUMN `player_id`;
-- reverse: modify "oauth_codes" table
ALTER TABLE `oauth_codes` DROP INDEX `oauthcode_player_id`, DROP COLUMN `auth_time`, DROP COLUMN `player_id`, DROP COLUMN `nonce`;
-- reverse: modify "oauth_clients" table
ALTER TABLE `oauth_clients` DROP INDEX `oauthclient_dynamic_last_used_at`, DROP COLUMN `last_used_at`, DROP COLUMN `dynamic`;
