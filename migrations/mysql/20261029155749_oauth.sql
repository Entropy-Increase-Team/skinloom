-- +goose Up
-- create "oauth_clients" table
CREATE TABLE `oauth_clients` (`id` bigint NOT NULL AUTO_INCREMENT, `name` varchar(64) NOT NULL, `secret_hash` varchar(64) NULL, `redirect_uris` json NOT NULL, `created_at` datetime(6) NOT NULL, `updated_at` datetime(6) NOT NULL, `user_id` bigint NOT NULL, PRIMARY KEY (`id`), INDEX `oauthclient_user_id` (`user_id`), CONSTRAINT `oauth_clients_users_oauth_clients` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "oauth_codes" table
CREATE TABLE `oauth_codes` (`id` bigint NOT NULL AUTO_INCREMENT, `code_hash` varchar(64) NOT NULL, `redirect_uri` varchar(2048) NOT NULL, `scopes` json NOT NULL, `code_challenge` varchar(128) NOT NULL DEFAULT "", `code_challenge_method` varchar(8) NOT NULL DEFAULT "", `expires_at` datetime(6) NOT NULL, `client_id` bigint NOT NULL, `user_id` bigint NOT NULL, PRIMARY KEY (`id`), UNIQUE INDEX `code_hash` (`code_hash`), INDEX `oauthcode_client_id` (`client_id`), INDEX `oauthcode_expires_at` (`expires_at`), INDEX `oauthcode_user_id` (`user_id`), CONSTRAINT `oauth_codes_oauth_clients_codes` FOREIGN KEY (`client_id`) REFERENCES `oauth_clients` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT `oauth_codes_users_oauth_codes` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "oauth_grants" table
CREATE TABLE `oauth_grants` (`id` bigint NOT NULL AUTO_INCREMENT, `scopes` json NOT NULL, `created_at` datetime(6) NOT NULL, `updated_at` datetime(6) NOT NULL, `client_id` bigint NOT NULL, `user_id` bigint NOT NULL, PRIMARY KEY (`id`), INDEX `oauthgrant_client_id` (`client_id`), UNIQUE INDEX `oauthgrant_user_id_client_id` (`user_id`, `client_id`), CONSTRAINT `oauth_grants_oauth_clients_grants` FOREIGN KEY (`client_id`) REFERENCES `oauth_clients` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT `oauth_grants_users_oauth_grants` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "oauth_tokens" table
CREATE TABLE `oauth_tokens` (`id` bigint NOT NULL AUTO_INCREMENT, `kind` enum('oauth','personal') NOT NULL, `token_hash` varchar(64) NOT NULL, `refresh_hash` varchar(64) NULL, `name` varchar(64) NOT NULL DEFAULT "", `scopes` json NOT NULL, `created_at` datetime(6) NOT NULL, `expires_at` datetime(6) NULL, `refresh_until` datetime(6) NULL, `last_used_at` datetime(6) NULL, `client_id` bigint NULL, `user_id` bigint NOT NULL, PRIMARY KEY (`id`), INDEX `oauthtoken_client_id_user_id` (`client_id`, `user_id`), INDEX `oauthtoken_expires_at` (`expires_at`), INDEX `oauthtoken_refresh_until` (`refresh_until`), INDEX `oauthtoken_user_id_kind` (`user_id`, `kind`), UNIQUE INDEX `refresh_hash` (`refresh_hash`), UNIQUE INDEX `token_hash` (`token_hash`), CONSTRAINT `oauth_tokens_oauth_clients_tokens` FOREIGN KEY (`client_id`) REFERENCES `oauth_clients` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT `oauth_tokens_users_oauth_tokens` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "oauth_tokens" table
DROP TABLE `oauth_tokens`;
-- reverse: create "oauth_grants" table
DROP TABLE `oauth_grants`;
-- reverse: create "oauth_codes" table
DROP TABLE `oauth_codes`;
-- reverse: create "oauth_clients" table
DROP TABLE `oauth_clients`;
