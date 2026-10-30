-- +goose Up
-- create "site_secrets" table
CREATE TABLE `site_secrets` (`key` varchar(64) NOT NULL, `value` longtext NOT NULL, `created_at` datetime(6) NOT NULL, PRIMARY KEY (`key`)) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "ygg_sessions" table
CREATE TABLE `ygg_sessions` (`id` bigint NOT NULL AUTO_INCREMENT, `server_id` varchar(128) NOT NULL, `ip` varchar(45) NOT NULL DEFAULT "", `expires_at` datetime(6) NOT NULL, `player_id` bigint NOT NULL, PRIMARY KEY (`id`), UNIQUE INDEX `server_id` (`server_id`), INDEX `yggsession_expires_at` (`expires_at`), INDEX `yggsession_player_id` (`player_id`), CONSTRAINT `ygg_sessions_players_ygg_sessions` FOREIGN KEY (`player_id`) REFERENCES `players` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "ygg_tokens" table
CREATE TABLE `ygg_tokens` (`id` bigint NOT NULL AUTO_INCREMENT, `token_hash` varchar(64) NOT NULL, `client_token` varchar(128) NOT NULL, `created_at` datetime(6) NOT NULL, `expires_at` datetime(6) NOT NULL, `refresh_until` datetime(6) NOT NULL, `player_id` bigint NULL, `user_id` bigint NOT NULL, PRIMARY KEY (`id`), UNIQUE INDEX `token_hash` (`token_hash`), INDEX `yggtoken_player_id` (`player_id`), INDEX `yggtoken_refresh_until` (`refresh_until`), INDEX `yggtoken_user_id_id` (`user_id`, `id`), CONSTRAINT `ygg_tokens_players_ygg_tokens` FOREIGN KEY (`player_id`) REFERENCES `players` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT `ygg_tokens_users_ygg_tokens` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "ygg_tokens" table
DROP TABLE `ygg_tokens`;
-- reverse: create "ygg_sessions" table
DROP TABLE `ygg_sessions`;
-- reverse: create "site_secrets" table
DROP TABLE `site_secrets`;
