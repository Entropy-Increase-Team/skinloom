-- +goose Up
-- create "oauth_clients" table
CREATE TABLE `oauth_clients` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `name` text NOT NULL, `secret_hash` text NULL, `redirect_uris` json NOT NULL, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `user_id` integer NOT NULL, CONSTRAINT `oauth_clients_users_oauth_clients` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "oauthclient_user_id" to table: "oauth_clients"
CREATE INDEX `oauthclient_user_id` ON `oauth_clients` (`user_id`);
-- create "oauth_codes" table
CREATE TABLE `oauth_codes` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `code_hash` text NOT NULL, `redirect_uri` text NOT NULL, `scopes` json NOT NULL, `code_challenge` text NOT NULL DEFAULT (''), `code_challenge_method` text NOT NULL DEFAULT (''), `expires_at` datetime NOT NULL, `client_id` integer NOT NULL, `user_id` integer NOT NULL, CONSTRAINT `oauth_codes_oauth_clients_codes` FOREIGN KEY (`client_id`) REFERENCES `oauth_clients` (`id`) ON DELETE CASCADE, CONSTRAINT `oauth_codes_users_oauth_codes` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "oauth_codes_code_hash_key" to table: "oauth_codes"
CREATE UNIQUE INDEX `oauth_codes_code_hash_key` ON `oauth_codes` (`code_hash`);
-- create index "oauthcode_client_id" to table: "oauth_codes"
CREATE INDEX `oauthcode_client_id` ON `oauth_codes` (`client_id`);
-- create index "oauthcode_user_id" to table: "oauth_codes"
CREATE INDEX `oauthcode_user_id` ON `oauth_codes` (`user_id`);
-- create index "oauthcode_expires_at" to table: "oauth_codes"
CREATE INDEX `oauthcode_expires_at` ON `oauth_codes` (`expires_at`);
-- create "oauth_grants" table
CREATE TABLE `oauth_grants` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `scopes` json NOT NULL, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `client_id` integer NOT NULL, `user_id` integer NOT NULL, CONSTRAINT `oauth_grants_oauth_clients_grants` FOREIGN KEY (`client_id`) REFERENCES `oauth_clients` (`id`) ON DELETE CASCADE, CONSTRAINT `oauth_grants_users_oauth_grants` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "oauthgrant_user_id_client_id" to table: "oauth_grants"
CREATE UNIQUE INDEX `oauthgrant_user_id_client_id` ON `oauth_grants` (`user_id`, `client_id`);
-- create index "oauthgrant_client_id" to table: "oauth_grants"
CREATE INDEX `oauthgrant_client_id` ON `oauth_grants` (`client_id`);
-- create "oauth_tokens" table
CREATE TABLE `oauth_tokens` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `kind` text NOT NULL, `token_hash` text NOT NULL, `refresh_hash` text NULL, `name` text NOT NULL DEFAULT (''), `scopes` json NOT NULL, `created_at` datetime NOT NULL, `expires_at` datetime NULL, `refresh_until` datetime NULL, `last_used_at` datetime NULL, `client_id` integer NULL, `user_id` integer NOT NULL, CONSTRAINT `oauth_tokens_oauth_clients_tokens` FOREIGN KEY (`client_id`) REFERENCES `oauth_clients` (`id`) ON DELETE CASCADE, CONSTRAINT `oauth_tokens_users_oauth_tokens` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "oauth_tokens_token_hash_key" to table: "oauth_tokens"
CREATE UNIQUE INDEX `oauth_tokens_token_hash_key` ON `oauth_tokens` (`token_hash`);
-- create index "oauth_tokens_refresh_hash_key" to table: "oauth_tokens"
CREATE UNIQUE INDEX `oauth_tokens_refresh_hash_key` ON `oauth_tokens` (`refresh_hash`);
-- create index "oauthtoken_user_id_kind" to table: "oauth_tokens"
CREATE INDEX `oauthtoken_user_id_kind` ON `oauth_tokens` (`user_id`, `kind`);
-- create index "oauthtoken_client_id_user_id" to table: "oauth_tokens"
CREATE INDEX `oauthtoken_client_id_user_id` ON `oauth_tokens` (`client_id`, `user_id`);
-- create index "oauthtoken_refresh_until" to table: "oauth_tokens"
CREATE INDEX `oauthtoken_refresh_until` ON `oauth_tokens` (`refresh_until`);
-- create index "oauthtoken_expires_at" to table: "oauth_tokens"
CREATE INDEX `oauthtoken_expires_at` ON `oauth_tokens` (`expires_at`);

-- +goose Down
-- reverse: create index "oauthtoken_expires_at" to table: "oauth_tokens"
DROP INDEX `oauthtoken_expires_at`;
-- reverse: create index "oauthtoken_refresh_until" to table: "oauth_tokens"
DROP INDEX `oauthtoken_refresh_until`;
-- reverse: create index "oauthtoken_client_id_user_id" to table: "oauth_tokens"
DROP INDEX `oauthtoken_client_id_user_id`;
-- reverse: create index "oauthtoken_user_id_kind" to table: "oauth_tokens"
DROP INDEX `oauthtoken_user_id_kind`;
-- reverse: create index "oauth_tokens_refresh_hash_key" to table: "oauth_tokens"
DROP INDEX `oauth_tokens_refresh_hash_key`;
-- reverse: create index "oauth_tokens_token_hash_key" to table: "oauth_tokens"
DROP INDEX `oauth_tokens_token_hash_key`;
-- reverse: create "oauth_tokens" table
DROP TABLE `oauth_tokens`;
-- reverse: create index "oauthgrant_client_id" to table: "oauth_grants"
DROP INDEX `oauthgrant_client_id`;
-- reverse: create index "oauthgrant_user_id_client_id" to table: "oauth_grants"
DROP INDEX `oauthgrant_user_id_client_id`;
-- reverse: create "oauth_grants" table
DROP TABLE `oauth_grants`;
-- reverse: create index "oauthcode_expires_at" to table: "oauth_codes"
DROP INDEX `oauthcode_expires_at`;
-- reverse: create index "oauthcode_user_id" to table: "oauth_codes"
DROP INDEX `oauthcode_user_id`;
-- reverse: create index "oauthcode_client_id" to table: "oauth_codes"
DROP INDEX `oauthcode_client_id`;
-- reverse: create index "oauth_codes_code_hash_key" to table: "oauth_codes"
DROP INDEX `oauth_codes_code_hash_key`;
-- reverse: create "oauth_codes" table
DROP TABLE `oauth_codes`;
-- reverse: create index "oauthclient_user_id" to table: "oauth_clients"
DROP INDEX `oauthclient_user_id`;
-- reverse: create "oauth_clients" table
DROP TABLE `oauth_clients`;
