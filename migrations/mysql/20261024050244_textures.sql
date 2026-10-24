-- +goose Up
-- create "blobs" table
CREATE TABLE `blobs` (`hash` varchar(64) NOT NULL, `size` bigint NOT NULL, `width` bigint NOT NULL, `height` bigint NOT NULL, `ref_count` bigint NOT NULL DEFAULT 0, `created_at` datetime(6) NOT NULL, PRIMARY KEY (`hash`)) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "textures" table
CREATE TABLE `textures` (`id` bigint NOT NULL AUTO_INCREMENT, `name` varchar(50) NOT NULL, `type` enum('steve','alex','cape') NOT NULL, `size` bigint NOT NULL, `visibility` enum('public','private') NOT NULL DEFAULT "public", `likes` bigint NOT NULL DEFAULT 0, `created_at` datetime(6) NOT NULL, `updated_at` datetime(6) NOT NULL, `hash` varchar(64) NOT NULL, `uploader_id` bigint NULL, PRIMARY KEY (`id`), INDEX `texture_hash` (`hash`), UNIQUE INDEX `texture_uploader_id_hash` (`uploader_id`, `hash`), INDEX `texture_visibility_id` (`visibility`, `id`), INDEX `texture_visibility_likes_id` (`visibility`, `likes`, `id`), INDEX `texture_visibility_type_id` (`visibility`, `type`, `id`), INDEX `texture_visibility_type_likes_id` (`visibility`, `type`, `likes`, `id`), CONSTRAINT `textures_blobs_textures` FOREIGN KEY (`hash`) REFERENCES `blobs` (`hash`) ON UPDATE NO ACTION ON DELETE NO ACTION, CONSTRAINT `textures_users_textures` FOREIGN KEY (`uploader_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "closet_items" table
CREATE TABLE `closet_items` (`id` bigint NOT NULL AUTO_INCREMENT, `name` varchar(50) NOT NULL, `created_at` datetime(6) NOT NULL, `texture_id` bigint NOT NULL, `user_id` bigint NOT NULL, PRIMARY KEY (`id`), INDEX `closetitem_texture_id` (`texture_id`), UNIQUE INDEX `closetitem_user_id_texture_id` (`user_id`, `texture_id`), CONSTRAINT `closet_items_textures_closet_items` FOREIGN KEY (`texture_id`) REFERENCES `textures` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT `closet_items_users_closet_items` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "score_ledger" table
CREATE TABLE `score_ledger` (`id` bigint NOT NULL AUTO_INCREMENT, `delta` bigint NOT NULL, `balance` bigint NOT NULL, `reason` varchar(64) NOT NULL, `ref_type` varchar(32) NOT NULL DEFAULT "", `ref_id` varchar(64) NOT NULL DEFAULT "", `actor_id` bigint NULL, `created_at` datetime(6) NOT NULL, `user_id` bigint NOT NULL, PRIMARY KEY (`id`), INDEX `scoreentry_user_id_id` (`user_id`, `id`), CONSTRAINT `score_ledger_users_score_entries` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "score_ledger" table
DROP TABLE `score_ledger`;
-- reverse: create "closet_items" table
DROP TABLE `closet_items`;
-- reverse: create "textures" table
DROP TABLE `textures`;
-- reverse: create "blobs" table
DROP TABLE `blobs`;
