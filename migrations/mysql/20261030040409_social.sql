-- +goose Up
-- create "social_accounts" table
CREATE TABLE `social_accounts` (`id` bigint NOT NULL AUTO_INCREMENT, `provider` varchar(32) NOT NULL, `subject` varchar(255) NOT NULL, `email` varchar(255) NOT NULL DEFAULT "", `name` varchar(255) NOT NULL DEFAULT "", `created_at` datetime(6) NOT NULL, `last_used_at` datetime(6) NULL, `user_id` bigint NOT NULL, PRIMARY KEY (`id`), UNIQUE INDEX `socialaccount_provider_subject` (`provider`, `subject`), UNIQUE INDEX `socialaccount_user_id_provider` (`user_id`, `provider`), CONSTRAINT `social_accounts_users_social_accounts` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "social_flows" table
CREATE TABLE `social_flows` (`id` varchar(64) NOT NULL, `purpose` enum('login','link','signup') NOT NULL, `provider` varchar(32) NOT NULL, `verifier` varchar(128) NOT NULL DEFAULT "", `nonce` varchar(64) NOT NULL DEFAULT "", `next` varchar(512) NOT NULL DEFAULT "", `remember` bool NOT NULL DEFAULT 0, `subject` varchar(255) NOT NULL DEFAULT "", `email` varchar(255) NOT NULL DEFAULT "", `email_verified` bool NOT NULL DEFAULT 0, `name` varchar(255) NOT NULL DEFAULT "", `nickname` varchar(255) NOT NULL DEFAULT "", `created_at` datetime(6) NOT NULL, `expires_at` datetime(6) NOT NULL, `user_id` bigint NULL, PRIMARY KEY (`id`), INDEX `socialflow_expires_at` (`expires_at`), INDEX `socialflow_user_id` (`user_id`), CONSTRAINT `social_flows_users_social_flows` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "social_flows" table
DROP TABLE `social_flows`;
-- reverse: create "social_accounts" table
DROP TABLE `social_accounts`;
