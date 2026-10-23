-- +goose Up
-- create "settings" table
CREATE TABLE `settings` (`key` varchar(128) NOT NULL, `value` longtext NOT NULL, `updated_by` bigint NULL, `updated_at` datetime(6) NOT NULL, PRIMARY KEY (`key`)) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "system_state" table
CREATE TABLE `system_state` (`key` varchar(64) NOT NULL, `int_value` bigint NOT NULL DEFAULT 0, `updated_at` datetime(6) NOT NULL, PRIMARY KEY (`key`)) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "system_state" table
DROP TABLE `system_state`;
-- reverse: create "settings" table
DROP TABLE `settings`;
