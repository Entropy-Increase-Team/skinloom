-- +goose Up
-- create "settings" table
CREATE TABLE `settings` (`key` text NOT NULL, `value` text NOT NULL, `updated_by` integer NULL, `updated_at` datetime NOT NULL, PRIMARY KEY (`key`));
-- create "system_state" table
CREATE TABLE `system_state` (`key` text NOT NULL, `int_value` integer NOT NULL DEFAULT (0), `updated_at` datetime NOT NULL, PRIMARY KEY (`key`));

-- +goose Down
-- reverse: create "system_state" table
DROP TABLE `system_state`;
-- reverse: create "settings" table
DROP TABLE `settings`;
