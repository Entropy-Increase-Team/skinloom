-- +goose Up
-- create "players" table
CREATE TABLE `players` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `name` text NOT NULL, `name_lower` text NOT NULL, `uuid` text NOT NULL, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL, `skin_id` integer NULL, `cape_id` integer NULL, `user_id` integer NOT NULL, CONSTRAINT `players_textures_skin_players` FOREIGN KEY (`skin_id`) REFERENCES `textures` (`id`) ON DELETE SET NULL, CONSTRAINT `players_textures_cape_players` FOREIGN KEY (`cape_id`) REFERENCES `textures` (`id`) ON DELETE SET NULL, CONSTRAINT `players_users_players` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "players_name_lower_key" to table: "players"
CREATE UNIQUE INDEX `players_name_lower_key` ON `players` (`name_lower`);
-- create index "players_uuid_key" to table: "players"
CREATE UNIQUE INDEX `players_uuid_key` ON `players` (`uuid`);
-- create index "player_user_id" to table: "players"
CREATE INDEX `player_user_id` ON `players` (`user_id`);
-- create index "player_skin_id" to table: "players"
CREATE INDEX `player_skin_id` ON `players` (`skin_id`);
-- create index "player_cape_id" to table: "players"
CREATE INDEX `player_cape_id` ON `players` (`cape_id`);

-- +goose Down
-- reverse: create index "player_cape_id" to table: "players"
DROP INDEX `player_cape_id`;
-- reverse: create index "player_skin_id" to table: "players"
DROP INDEX `player_skin_id`;
-- reverse: create index "player_user_id" to table: "players"
DROP INDEX `player_user_id`;
-- reverse: create index "players_uuid_key" to table: "players"
DROP INDEX `players_uuid_key`;
-- reverse: create index "players_name_lower_key" to table: "players"
DROP INDEX `players_name_lower_key`;
-- reverse: create "players" table
DROP TABLE `players`;
