-- +goose Up
-- add OIDC/DCR/player-binding columns (ADD COLUMN, not atlas's table rebuild: goose runs
-- each file in a transaction with foreign keys on, where DROP TABLE violates TestSQLiteMigrationsDoNotRebuildTables)
ALTER TABLE `oauth_clients` ADD COLUMN `dynamic` bool NOT NULL DEFAULT (false);
ALTER TABLE `oauth_clients` ADD COLUMN `last_used_at` datetime NULL;
CREATE INDEX `oauthclient_dynamic_last_used_at` ON `oauth_clients` (`dynamic`, `last_used_at`);

ALTER TABLE `oauth_codes` ADD COLUMN `nonce` text NOT NULL DEFAULT ('');
ALTER TABLE `oauth_codes` ADD COLUMN `player_id` integer NULL;
ALTER TABLE `oauth_codes` ADD COLUMN `auth_time` datetime NULL;
CREATE INDEX `oauthcode_player_id` ON `oauth_codes` (`player_id`);

ALTER TABLE `oauth_tokens` ADD COLUMN `player_id` integer NULL;
ALTER TABLE `oauth_tokens` ADD COLUMN `player_name_at_issue` text NOT NULL DEFAULT ('');
CREATE INDEX `oauthtoken_player_id` ON `oauth_tokens` (`player_id`);

-- create "oauth_device_codes" table
CREATE TABLE `oauth_device_codes` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `device_code_hash` text NOT NULL, `user_code_hash` text NOT NULL, `scopes` json NOT NULL, `status` text NOT NULL DEFAULT ('pending'), `player_id` integer NULL, `interval_sec` integer NOT NULL DEFAULT (5), `last_polled_at` datetime NULL, `expires_at` datetime NOT NULL, `created_at` datetime NOT NULL, `client_id` integer NOT NULL, `user_id` integer NULL, CONSTRAINT `oauth_device_codes_oauth_clients_device_codes` FOREIGN KEY (`client_id`) REFERENCES `oauth_clients` (`id`) ON DELETE CASCADE, CONSTRAINT `oauth_device_codes_users_oauth_device_codes` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
CREATE UNIQUE INDEX `oauth_device_codes_device_code_hash_key` ON `oauth_device_codes` (`device_code_hash`);
CREATE UNIQUE INDEX `oauth_device_codes_user_code_hash_key` ON `oauth_device_codes` (`user_code_hash`);
CREATE INDEX `oauthdevicecode_client_id` ON `oauth_device_codes` (`client_id`);
CREATE INDEX `oauthdevicecode_user_id` ON `oauth_device_codes` (`user_id`);
CREATE INDEX `oauthdevicecode_player_id` ON `oauth_device_codes` (`player_id`);
CREATE INDEX `oauthdevicecode_expires_at` ON `oauth_device_codes` (`expires_at`);

-- +goose Down
DROP INDEX `oauthdevicecode_expires_at`;
DROP INDEX `oauthdevicecode_player_id`;
DROP INDEX `oauthdevicecode_user_id`;
DROP INDEX `oauthdevicecode_client_id`;
DROP INDEX `oauth_device_codes_user_code_hash_key`;
DROP INDEX `oauth_device_codes_device_code_hash_key`;
DROP TABLE `oauth_device_codes`;
DROP INDEX `oauthtoken_player_id`;
ALTER TABLE `oauth_tokens` DROP COLUMN `player_name_at_issue`;
ALTER TABLE `oauth_tokens` DROP COLUMN `player_id`;
DROP INDEX `oauthcode_player_id`;
ALTER TABLE `oauth_codes` DROP COLUMN `auth_time`;
ALTER TABLE `oauth_codes` DROP COLUMN `player_id`;
ALTER TABLE `oauth_codes` DROP COLUMN `nonce`;
DROP INDEX `oauthclient_dynamic_last_used_at`;
ALTER TABLE `oauth_clients` DROP COLUMN `last_used_at`;
ALTER TABLE `oauth_clients` DROP COLUMN `dynamic`;
