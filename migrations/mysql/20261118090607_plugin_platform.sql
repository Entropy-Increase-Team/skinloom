-- +goose Up
-- create "plugins" table
CREATE TABLE `plugins` (`id` varchar(40) NOT NULL, `kind` enum('plugin','theme') NOT NULL DEFAULT "plugin", `version` varchar(64) NOT NULL, `status` enum('enabled','disabled','failed') NOT NULL DEFAULT "disabled", `granted_permissions` json NULL, `settings` json NULL, `error` varchar(1024) NULL DEFAULT "", `source` varchar(32) NOT NULL DEFAULT "package", `package_sha256` varchar(64) NULL, `installed_at` datetime(6) NOT NULL, `updated_at` datetime(6) NOT NULL, PRIMARY KEY (`id`)) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "plugin_docs" table
CREATE TABLE `plugin_docs` (`id` bigint NOT NULL AUTO_INCREMENT, `plugin` varchar(40) NOT NULL, `collection` varchar(64) NOT NULL, `doc_id` varchar(64) NOT NULL, `data` json NOT NULL, `u1` varchar(255) NULL, `u2` varchar(255) NULL, `s1` varchar(255) NULL, `s2` varchar(255) NULL, `s3` varchar(255) NULL, `n1` double NULL, `n2` double NULL, `n3` double NULL, `t1` datetime(6) NULL, `t2` datetime(6) NULL, `created_at` datetime(6) NOT NULL, `updated_at` datetime(6) NOT NULL, PRIMARY KEY (`id`), UNIQUE INDEX `plugindoc_plugin_collection_doc_id` (`plugin`, `collection`, `doc_id`), UNIQUE INDEX `plugindoc_plugin_collection_u1` (`plugin`, `collection`, `u1`), UNIQUE INDEX `plugindoc_plugin_collection_u2` (`plugin`, `collection`, `u2`), INDEX `plugindoc_plugin_collection_s1` (`plugin`, `collection`, `s1`), INDEX `plugindoc_plugin_collection_s2` (`plugin`, `collection`, `s2`), INDEX `plugindoc_plugin_collection_s3` (`plugin`, `collection`, `s3`), INDEX `plugindoc_plugin_collection_n1` (`plugin`, `collection`, `n1`), INDEX `plugindoc_plugin_collection_n2` (`plugin`, `collection`, `n2`), INDEX `plugindoc_plugin_collection_n3` (`plugin`, `collection`, `n3`), INDEX `plugindoc_plugin_collection_t1` (`plugin`, `collection`, `t1`), INDEX `plugindoc_plugin_collection_t2` (`plugin`, `collection`, `t2`)) CHARSET utf8mb4 COLLATE utf8mb4_bin;
-- create "plugin_kv" table
CREATE TABLE `plugin_kv` (`id` bigint NOT NULL AUTO_INCREMENT, `plugin` varchar(40) NOT NULL, `key` varchar(255) NOT NULL, `value` json NOT NULL, `expires_at` datetime(6) NULL, `updated_at` datetime(6) NOT NULL, PRIMARY KEY (`id`), INDEX `pluginkv_expires_at` (`expires_at`), UNIQUE INDEX `pluginkv_plugin_key` (`plugin`, `key`)) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "plugin_kv" table
DROP TABLE `plugin_kv`;
-- reverse: create "plugin_docs" table
DROP TABLE `plugin_docs`;
-- reverse: create "plugins" table
DROP TABLE `plugins`;
