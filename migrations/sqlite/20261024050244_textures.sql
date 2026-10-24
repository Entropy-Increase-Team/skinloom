-- +goose Up
-- create "blobs" table
CREATE TABLE `blobs` (`hash` text NOT NULL, `size` integer NOT NULL, `width` integer NOT NULL, `height` integer NOT NULL, `ref_count` integer NOT NULL DEFAULT (0), `created_at` datetime NOT NULL, PRIMARY KEY (`hash`));
-- create "closet_items" table
CREATE TABLE `closet_items` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `name` text NOT NULL, `created_at` datetime NOT NULL, `texture_id` integer NOT NULL, `user_id` integer NOT NULL, CONSTRAINT `closet_items_textures_closet_items` FOREIGN KEY (`texture_id`) REFERENCES `textures` (`id`) ON DELETE CASCADE, CONSTRAINT `closet_items_users_closet_items` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "closetitem_user_id_texture_id" to table: "closet_items"
CREATE UNIQUE INDEX `closetitem_user_id_texture_id` ON `closet_items` (`user_id`, `texture_id`);
-- create index "closetitem_texture_id" to table: "closet_items"
CREATE INDEX `closetitem_texture_id` ON `closet_items` (`texture_id`);
-- create "score_ledger" table
CREATE TABLE `score_ledger` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `delta` integer NOT NULL, `balance` integer NOT NULL, `reason` text NOT NULL, `ref_type` text NOT NULL DEFAULT (''), `ref_id` text NOT NULL DEFAULT (''), `actor_id` integer NULL, `created_at` datetime NOT NULL, `user_id` integer NOT NULL, CONSTRAINT `score_ledger_users_score_entries` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "scoreentry_user_id_id" to table: "score_ledger"
CREATE INDEX `scoreentry_user_id_id` ON `score_ledger` (`user_id`, `id`);
-- create "textures" table
CREATE TABLE `textures` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `name` text NOT NULL, `type` text NOT NULL, `size` integer NOT NULL, `visibility` text NOT NULL DEFAULT ('public'), `likes` integer NOT NULL DEFAULT (0), `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `hash` text NOT NULL, `uploader_id` integer NULL, CONSTRAINT `textures_blobs_textures` FOREIGN KEY (`hash`) REFERENCES `blobs` (`hash`) ON DELETE NO ACTION, CONSTRAINT `textures_users_textures` FOREIGN KEY (`uploader_id`) REFERENCES `users` (`id`) ON DELETE SET NULL);
-- create index "texture_visibility_id" to table: "textures"
CREATE INDEX `texture_visibility_id` ON `textures` (`visibility`, `id`);
-- create index "texture_visibility_likes_id" to table: "textures"
CREATE INDEX `texture_visibility_likes_id` ON `textures` (`visibility`, `likes`, `id`);
-- create index "texture_visibility_type_id" to table: "textures"
CREATE INDEX `texture_visibility_type_id` ON `textures` (`visibility`, `type`, `id`);
-- create index "texture_visibility_type_likes_id" to table: "textures"
CREATE INDEX `texture_visibility_type_likes_id` ON `textures` (`visibility`, `type`, `likes`, `id`);
-- create index "texture_uploader_id_hash" to table: "textures"
CREATE UNIQUE INDEX `texture_uploader_id_hash` ON `textures` (`uploader_id`, `hash`);
-- create index "texture_hash" to table: "textures"
CREATE INDEX `texture_hash` ON `textures` (`hash`);

-- +goose Down
-- reverse: create index "texture_hash" to table: "textures"
DROP INDEX `texture_hash`;
-- reverse: create index "texture_uploader_id_hash" to table: "textures"
DROP INDEX `texture_uploader_id_hash`;
-- reverse: create index "texture_visibility_type_likes_id" to table: "textures"
DROP INDEX `texture_visibility_type_likes_id`;
-- reverse: create index "texture_visibility_type_id" to table: "textures"
DROP INDEX `texture_visibility_type_id`;
-- reverse: create index "texture_visibility_likes_id" to table: "textures"
DROP INDEX `texture_visibility_likes_id`;
-- reverse: create index "texture_visibility_id" to table: "textures"
DROP INDEX `texture_visibility_id`;
-- reverse: create "textures" table
DROP TABLE `textures`;
-- reverse: create index "scoreentry_user_id_id" to table: "score_ledger"
DROP INDEX `scoreentry_user_id_id`;
-- reverse: create "score_ledger" table
DROP TABLE `score_ledger`;
-- reverse: create index "closetitem_texture_id" to table: "closet_items"
DROP INDEX `closetitem_texture_id`;
-- reverse: create index "closetitem_user_id_texture_id" to table: "closet_items"
DROP INDEX `closetitem_user_id_texture_id`;
-- reverse: create "closet_items" table
DROP TABLE `closet_items`;
-- reverse: create "blobs" table
DROP TABLE `blobs`;
