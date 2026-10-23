-- +goose Up
-- create "audit_logs" table
CREATE TABLE `audit_logs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `actor_id` integer NULL, `action` text NOT NULL, `target_type` text NOT NULL DEFAULT (''), `target_id` text NOT NULL DEFAULT (''), `ip` text NOT NULL DEFAULT (''), `meta` json NULL, `created_at` datetime NOT NULL);
-- create index "auditlog_actor_id_created_at" to table: "audit_logs"
CREATE INDEX `auditlog_actor_id_created_at` ON `audit_logs` (`actor_id`, `created_at`);
-- create index "auditlog_action_created_at" to table: "audit_logs"
CREATE INDEX `auditlog_action_created_at` ON `audit_logs` (`action`, `created_at`);
-- create "auth_tokens" table
CREATE TABLE `auth_tokens` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `purpose` text NOT NULL, `token_hash` text NOT NULL, `expires_at` datetime NOT NULL, `used_at` datetime NULL, `created_at` datetime NOT NULL, `user_id` integer NULL, CONSTRAINT `auth_tokens_users_auth_tokens` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "auth_tokens_token_hash_key" to table: "auth_tokens"
CREATE UNIQUE INDEX `auth_tokens_token_hash_key` ON `auth_tokens` (`token_hash`);
-- create index "authtoken_user_id_purpose" to table: "auth_tokens"
CREATE INDEX `authtoken_user_id_purpose` ON `auth_tokens` (`user_id`, `purpose`);
-- create "sessions" table
CREATE TABLE `sessions` (`id` text NOT NULL, `ip` text NOT NULL DEFAULT (''), `user_agent` text NOT NULL DEFAULT (''), `created_at` datetime NOT NULL, `last_seen_at` datetime NOT NULL, `expires_at` datetime NOT NULL, `revoked_at` datetime NULL, `user_id` integer NOT NULL, PRIMARY KEY (`id`), CONSTRAINT `sessions_users_sessions` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "session_user_id" to table: "sessions"
CREATE INDEX `session_user_id` ON `sessions` (`user_id`);
-- create "users" table
CREATE TABLE `users` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `email` text NOT NULL, `email_normalized` text NOT NULL, `nickname` text NOT NULL, `password_hash` text NOT NULL, `password_algo` text NOT NULL DEFAULT ('argon2id'), `role` text NOT NULL DEFAULT ('user'), `status` text NOT NULL DEFAULT ('active'), `score` integer NOT NULL DEFAULT (0), `locale` text NOT NULL DEFAULT (''), `email_verified_at` datetime NULL, `register_ip` text NOT NULL DEFAULT (''), `last_login_at` datetime NULL, `deleted_at` datetime NULL, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL);
-- create index "users_email_normalized_key" to table: "users"
CREATE UNIQUE INDEX `users_email_normalized_key` ON `users` (`email_normalized`);
-- create index "user_register_ip_created_at" to table: "users"
CREATE INDEX `user_register_ip_created_at` ON `users` (`register_ip`, `created_at`);

-- +goose Down
-- reverse: create index "user_register_ip_created_at" to table: "users"
DROP INDEX `user_register_ip_created_at`;
-- reverse: create index "users_email_normalized_key" to table: "users"
DROP INDEX `users_email_normalized_key`;
-- reverse: create "users" table
DROP TABLE `users`;
-- reverse: create index "session_user_id" to table: "sessions"
DROP INDEX `session_user_id`;
-- reverse: create "sessions" table
DROP TABLE `sessions`;
-- reverse: create index "authtoken_user_id_purpose" to table: "auth_tokens"
DROP INDEX `authtoken_user_id_purpose`;
-- reverse: create index "auth_tokens_token_hash_key" to table: "auth_tokens"
DROP INDEX `auth_tokens_token_hash_key`;
-- reverse: create "auth_tokens" table
DROP TABLE `auth_tokens`;
-- reverse: create index "auditlog_action_created_at" to table: "audit_logs"
DROP INDEX `auditlog_action_created_at`;
-- reverse: create index "auditlog_actor_id_created_at" to table: "audit_logs"
DROP INDEX `auditlog_actor_id_created_at`;
-- reverse: create "audit_logs" table
DROP TABLE `audit_logs`;
