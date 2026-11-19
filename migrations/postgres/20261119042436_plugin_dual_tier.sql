-- +goose Up
-- modify "plugins" table
ALTER TABLE "plugins" ADD COLUMN "runtime" character varying NOT NULL DEFAULT 'js', ADD COLUMN "platform_packages" jsonb NULL, ADD COLUMN "registry_source" character varying NULL DEFAULT '';

-- +goose Down
-- reverse: modify "plugins" table
ALTER TABLE "plugins" DROP COLUMN "registry_source", DROP COLUMN "platform_packages", DROP COLUMN "runtime";
