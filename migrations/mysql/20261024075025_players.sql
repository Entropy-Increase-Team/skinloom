-- +goose Up
-- create "players" table
CREATE TABLE `players` (`id` bigint NOT NULL AUTO_INCREMENT, `name` varchar(32) NOT NULL, `name_lower` varchar(32) NOT NULL, `uuid` varchar(32) NOT NULL, `created_at` datetime(6) NOT NULL, `updated_at` datetime(6) NOT NULL, `skin_id` bigint NULL, `cape_id` bigint NULL, `user_id` bigint NOT NULL, PRIMARY KEY (`id`), INDEX `player_cape_id` (`cape_id`), INDEX `player_skin_id` (`skin_id`), INDEX `player_user_id` (`user_id`), UNIQUE INDEX `name_lower` (`name_lower`), UNIQUE INDEX `uuid` (`uuid`), CONSTRAINT `players_textures_cape_players` FOREIGN KEY (`cape_id`) REFERENCES `textures` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT `players_textures_skin_players` FOREIGN KEY (`skin_id`) REFERENCES `textures` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL, CONSTRAINT `players_users_players` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "players" table
DROP TABLE `players`;
