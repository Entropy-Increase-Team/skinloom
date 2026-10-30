-- +goose Up
-- create "social_accounts" table
CREATE TABLE `social_accounts` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `provider` text NOT NULL, `subject` text NOT NULL, `email` text NOT NULL DEFAULT (''), `name` text NOT NULL DEFAULT (''), `created_at` datetime NOT NULL, `last_used_at` datetime NULL, `user_id` integer NOT NULL, CONSTRAINT `social_accounts_users_social_accounts` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "socialaccount_provider_subject" to table: "social_accounts"
CREATE UNIQUE INDEX `socialaccount_provider_subject` ON `social_accounts` (`provider`, `subject`);
-- create index "socialaccount_user_id_provider" to table: "social_accounts"
CREATE UNIQUE INDEX `socialaccount_user_id_provider` ON `social_accounts` (`user_id`, `provider`);
-- create "social_flows" table
CREATE TABLE `social_flows` (`id` text NOT NULL, `purpose` text NOT NULL, `provider` text NOT NULL, `verifier` text NOT NULL DEFAULT (''), `nonce` text NOT NULL DEFAULT (''), `next` text NOT NULL DEFAULT (''), `remember` bool NOT NULL DEFAULT (false), `subject` text NOT NULL DEFAULT (''), `email` text NOT NULL DEFAULT (''), `email_verified` bool NOT NULL DEFAULT (false), `name` text NOT NULL DEFAULT (''), `nickname` text NOT NULL DEFAULT (''), `created_at` datetime NOT NULL, `expires_at` datetime NOT NULL, `user_id` integer NULL, PRIMARY KEY (`id`), CONSTRAINT `social_flows_users_social_flows` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "socialflow_user_id" to table: "social_flows"
CREATE INDEX `socialflow_user_id` ON `social_flows` (`user_id`);
-- create index "socialflow_expires_at" to table: "social_flows"
CREATE INDEX `socialflow_expires_at` ON `social_flows` (`expires_at`);

-- +goose Down
-- reverse: create index "socialflow_expires_at" to table: "social_flows"
DROP INDEX `socialflow_expires_at`;
-- reverse: create index "socialflow_user_id" to table: "social_flows"
DROP INDEX `socialflow_user_id`;
-- reverse: create "social_flows" table
DROP TABLE `social_flows`;
-- reverse: create index "socialaccount_user_id_provider" to table: "social_accounts"
DROP INDEX `socialaccount_user_id_provider`;
-- reverse: create index "socialaccount_provider_subject" to table: "social_accounts"
DROP INDEX `socialaccount_provider_subject`;
-- reverse: create "social_accounts" table
DROP TABLE `social_accounts`;
