-- +goose Up
-- create "site_secrets" table
CREATE TABLE `site_secrets` (`key` text NOT NULL, `value` text NOT NULL, `created_at` datetime NOT NULL, PRIMARY KEY (`key`));
-- create "ygg_sessions" table
CREATE TABLE `ygg_sessions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `server_id` text NOT NULL, `ip` text NOT NULL DEFAULT (''), `expires_at` datetime NOT NULL, `player_id` integer NOT NULL, CONSTRAINT `ygg_sessions_players_ygg_sessions` FOREIGN KEY (`player_id`) REFERENCES `players` (`id`) ON DELETE CASCADE);
-- create index "ygg_sessions_server_id_key" to table: "ygg_sessions"
CREATE UNIQUE INDEX `ygg_sessions_server_id_key` ON `ygg_sessions` (`server_id`);
-- create index "yggsession_player_id" to table: "ygg_sessions"
CREATE INDEX `yggsession_player_id` ON `ygg_sessions` (`player_id`);
-- create index "yggsession_expires_at" to table: "ygg_sessions"
CREATE INDEX `yggsession_expires_at` ON `ygg_sessions` (`expires_at`);
-- create "ygg_tokens" table
CREATE TABLE `ygg_tokens` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `token_hash` text NOT NULL, `client_token` text NOT NULL, `created_at` datetime NOT NULL, `expires_at` datetime NOT NULL, `refresh_until` datetime NOT NULL, `player_id` integer NULL, `user_id` integer NOT NULL, CONSTRAINT `ygg_tokens_players_ygg_tokens` FOREIGN KEY (`player_id`) REFERENCES `players` (`id`) ON DELETE CASCADE, CONSTRAINT `ygg_tokens_users_ygg_tokens` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "ygg_tokens_token_hash_key" to table: "ygg_tokens"
CREATE UNIQUE INDEX `ygg_tokens_token_hash_key` ON `ygg_tokens` (`token_hash`);
-- create index "yggtoken_user_id_id" to table: "ygg_tokens"
CREATE INDEX `yggtoken_user_id_id` ON `ygg_tokens` (`user_id`, `id`);
-- create index "yggtoken_player_id" to table: "ygg_tokens"
CREATE INDEX `yggtoken_player_id` ON `ygg_tokens` (`player_id`);
-- create index "yggtoken_refresh_until" to table: "ygg_tokens"
CREATE INDEX `yggtoken_refresh_until` ON `ygg_tokens` (`refresh_until`);

-- +goose Down
-- reverse: create index "yggtoken_refresh_until" to table: "ygg_tokens"
DROP INDEX `yggtoken_refresh_until`;
-- reverse: create index "yggtoken_player_id" to table: "ygg_tokens"
DROP INDEX `yggtoken_player_id`;
-- reverse: create index "yggtoken_user_id_id" to table: "ygg_tokens"
DROP INDEX `yggtoken_user_id_id`;
-- reverse: create index "ygg_tokens_token_hash_key" to table: "ygg_tokens"
DROP INDEX `ygg_tokens_token_hash_key`;
-- reverse: create "ygg_tokens" table
DROP TABLE `ygg_tokens`;
-- reverse: create index "yggsession_expires_at" to table: "ygg_sessions"
DROP INDEX `yggsession_expires_at`;
-- reverse: create index "yggsession_player_id" to table: "ygg_sessions"
DROP INDEX `yggsession_player_id`;
-- reverse: create index "ygg_sessions_server_id_key" to table: "ygg_sessions"
DROP INDEX `ygg_sessions_server_id_key`;
-- reverse: create "ygg_sessions" table
DROP TABLE `ygg_sessions`;
-- reverse: create "site_secrets" table
DROP TABLE `site_secrets`;
