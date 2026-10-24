-- +goose Up
-- add column "last_sign_at" to table: "users"
ALTER TABLE `users` ADD COLUMN `last_sign_at` datetime NULL;

-- +goose Down
-- reverse: add column "last_sign_at" to table: "users"
ALTER TABLE `users` DROP COLUMN `last_sign_at`;
