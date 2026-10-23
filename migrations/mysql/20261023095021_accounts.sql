-- +goose Up
-- create "audit_logs" table
CREATE TABLE `audit_logs` (`id` bigint NOT NULL AUTO_INCREMENT, `actor_id` bigint NULL, `action` varchar(64) NOT NULL, `target_type` varchar(32) NOT NULL DEFAULT "", `target_id` varchar(64) NOT NULL DEFAULT "", `ip` varchar(45) NOT NULL DEFAULT "", `meta` json NULL, `created_at` datetime(6) NOT NULL, PRIMARY KEY (`id`), INDEX `auditlog_action_created_at` (`action`, `created_at`), INDEX `auditlog_actor_id_created_at` (`actor_id`, `created_at`)) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "users" table
CREATE TABLE `users` (`id` bigint NOT NULL AUTO_INCREMENT, `email` varchar(255) NOT NULL, `email_normalized` varchar(255) NOT NULL, `nickname` varchar(64) NOT NULL, `password_hash` varchar(255) NOT NULL, `password_algo` varchar(32) NOT NULL DEFAULT "argon2id", `role` enum('user','admin','superadmin') NOT NULL DEFAULT "user", `status` enum('active','banned','deleted') NOT NULL DEFAULT "active", `score` bigint NOT NULL DEFAULT 0, `locale` varchar(16) NOT NULL DEFAULT "", `email_verified_at` datetime(6) NULL, `register_ip` varchar(45) NOT NULL DEFAULT "", `last_login_at` datetime(6) NULL, `deleted_at` datetime(6) NULL, `created_at` datetime(6) NOT NULL, `updated_at` datetime(6) NOT NULL, PRIMARY KEY (`id`), INDEX `user_register_ip_created_at` (`register_ip`, `created_at`), UNIQUE INDEX `email_normalized` (`email_normalized`)) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "auth_tokens" table
CREATE TABLE `auth_tokens` (`id` bigint NOT NULL AUTO_INCREMENT, `purpose` enum('setup','verify_email','reset_password') NOT NULL, `token_hash` varchar(64) NOT NULL, `expires_at` datetime(6) NOT NULL, `used_at` datetime(6) NULL, `created_at` datetime(6) NOT NULL, `user_id` bigint NULL, PRIMARY KEY (`id`), INDEX `authtoken_user_id_purpose` (`user_id`, `purpose`), UNIQUE INDEX `token_hash` (`token_hash`), CONSTRAINT `auth_tokens_users_auth_tokens` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "sessions" table
CREATE TABLE `sessions` (`id` varchar(64) NOT NULL, `ip` varchar(45) NOT NULL DEFAULT "", `user_agent` varchar(512) NOT NULL DEFAULT "", `created_at` datetime(6) NOT NULL, `last_seen_at` datetime(6) NOT NULL, `expires_at` datetime(6) NOT NULL, `revoked_at` datetime(6) NULL, `user_id` bigint NOT NULL, PRIMARY KEY (`id`), INDEX `session_user_id` (`user_id`), CONSTRAINT `sessions_users_sessions` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "sessions" table
DROP TABLE `sessions`;
-- reverse: create "auth_tokens" table
DROP TABLE `auth_tokens`;
-- reverse: create "users" table
DROP TABLE `users`;
-- reverse: create "audit_logs" table
DROP TABLE `audit_logs`;
