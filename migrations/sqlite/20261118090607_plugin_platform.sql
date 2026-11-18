-- +goose Up
-- create "plugins" table
CREATE TABLE `plugins` (`id` text NOT NULL, `kind` text NOT NULL DEFAULT ('plugin'), `version` text NOT NULL, `status` text NOT NULL DEFAULT ('disabled'), `granted_permissions` json NULL, `settings` json NULL, `error` text NULL DEFAULT (''), `source` text NOT NULL DEFAULT ('package'), `package_sha256` text NULL, `installed_at` datetime NOT NULL, `updated_at` datetime NOT NULL, PRIMARY KEY (`id`));
-- create "plugin_docs" table
CREATE TABLE `plugin_docs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `plugin` text NOT NULL, `collection` text NOT NULL, `doc_id` text NOT NULL, `data` json NOT NULL, `u1` text NULL, `u2` text NULL, `s1` text NULL, `s2` text NULL, `s3` text NULL, `n1` real NULL, `n2` real NULL, `n3` real NULL, `t1` datetime NULL, `t2` datetime NULL, `created_at` datetime NOT NULL, `updated_at` datetime NOT NULL);
-- create index "plugindoc_plugin_collection_doc_id" to table: "plugin_docs"
CREATE UNIQUE INDEX `plugindoc_plugin_collection_doc_id` ON `plugin_docs` (`plugin`, `collection`, `doc_id`);
-- create index "plugindoc_plugin_collection_u1" to table: "plugin_docs"
CREATE UNIQUE INDEX `plugindoc_plugin_collection_u1` ON `plugin_docs` (`plugin`, `collection`, `u1`);
-- create index "plugindoc_plugin_collection_u2" to table: "plugin_docs"
CREATE UNIQUE INDEX `plugindoc_plugin_collection_u2` ON `plugin_docs` (`plugin`, `collection`, `u2`);
-- create index "plugindoc_plugin_collection_s1" to table: "plugin_docs"
CREATE INDEX `plugindoc_plugin_collection_s1` ON `plugin_docs` (`plugin`, `collection`, `s1`);
-- create index "plugindoc_plugin_collection_s2" to table: "plugin_docs"
CREATE INDEX `plugindoc_plugin_collection_s2` ON `plugin_docs` (`plugin`, `collection`, `s2`);
-- create index "plugindoc_plugin_collection_s3" to table: "plugin_docs"
CREATE INDEX `plugindoc_plugin_collection_s3` ON `plugin_docs` (`plugin`, `collection`, `s3`);
-- create index "plugindoc_plugin_collection_n1" to table: "plugin_docs"
CREATE INDEX `plugindoc_plugin_collection_n1` ON `plugin_docs` (`plugin`, `collection`, `n1`);
-- create index "plugindoc_plugin_collection_n2" to table: "plugin_docs"
CREATE INDEX `plugindoc_plugin_collection_n2` ON `plugin_docs` (`plugin`, `collection`, `n2`);
-- create index "plugindoc_plugin_collection_n3" to table: "plugin_docs"
CREATE INDEX `plugindoc_plugin_collection_n3` ON `plugin_docs` (`plugin`, `collection`, `n3`);
-- create index "plugindoc_plugin_collection_t1" to table: "plugin_docs"
CREATE INDEX `plugindoc_plugin_collection_t1` ON `plugin_docs` (`plugin`, `collection`, `t1`);
-- create index "plugindoc_plugin_collection_t2" to table: "plugin_docs"
CREATE INDEX `plugindoc_plugin_collection_t2` ON `plugin_docs` (`plugin`, `collection`, `t2`);
-- create "plugin_kv" table
CREATE TABLE `plugin_kv` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `plugin` text NOT NULL, `key` text NOT NULL, `value` json NOT NULL, `expires_at` datetime NULL, `updated_at` datetime NOT NULL);
-- create index "pluginkv_plugin_key" to table: "plugin_kv"
CREATE UNIQUE INDEX `pluginkv_plugin_key` ON `plugin_kv` (`plugin`, `key`);
-- create index "pluginkv_expires_at" to table: "plugin_kv"
CREATE INDEX `pluginkv_expires_at` ON `plugin_kv` (`expires_at`);

-- +goose Down
-- reverse: create index "pluginkv_expires_at" to table: "plugin_kv"
DROP INDEX `pluginkv_expires_at`;
-- reverse: create index "pluginkv_plugin_key" to table: "plugin_kv"
DROP INDEX `pluginkv_plugin_key`;
-- reverse: create "plugin_kv" table
DROP TABLE `plugin_kv`;
-- reverse: create index "plugindoc_plugin_collection_t2" to table: "plugin_docs"
DROP INDEX `plugindoc_plugin_collection_t2`;
-- reverse: create index "plugindoc_plugin_collection_t1" to table: "plugin_docs"
DROP INDEX `plugindoc_plugin_collection_t1`;
-- reverse: create index "plugindoc_plugin_collection_n3" to table: "plugin_docs"
DROP INDEX `plugindoc_plugin_collection_n3`;
-- reverse: create index "plugindoc_plugin_collection_n2" to table: "plugin_docs"
DROP INDEX `plugindoc_plugin_collection_n2`;
-- reverse: create index "plugindoc_plugin_collection_n1" to table: "plugin_docs"
DROP INDEX `plugindoc_plugin_collection_n1`;
-- reverse: create index "plugindoc_plugin_collection_s3" to table: "plugin_docs"
DROP INDEX `plugindoc_plugin_collection_s3`;
-- reverse: create index "plugindoc_plugin_collection_s2" to table: "plugin_docs"
DROP INDEX `plugindoc_plugin_collection_s2`;
-- reverse: create index "plugindoc_plugin_collection_s1" to table: "plugin_docs"
DROP INDEX `plugindoc_plugin_collection_s1`;
-- reverse: create index "plugindoc_plugin_collection_u2" to table: "plugin_docs"
DROP INDEX `plugindoc_plugin_collection_u2`;
-- reverse: create index "plugindoc_plugin_collection_u1" to table: "plugin_docs"
DROP INDEX `plugindoc_plugin_collection_u1`;
-- reverse: create index "plugindoc_plugin_collection_doc_id" to table: "plugin_docs"
DROP INDEX `plugindoc_plugin_collection_doc_id`;
-- reverse: create "plugin_docs" table
DROP TABLE `plugin_docs`;
-- reverse: create "plugins" table
DROP TABLE `plugins`;
