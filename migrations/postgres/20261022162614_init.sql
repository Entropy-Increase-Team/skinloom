-- +goose Up
-- create "settings" table
CREATE TABLE "settings" ("key" character varying NOT NULL, "value" text NOT NULL, "updated_by" bigint NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("key"));
-- create "system_state" table
CREATE TABLE "system_state" ("key" character varying NOT NULL, "int_value" bigint NOT NULL DEFAULT 0, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("key"));

-- +goose Down
-- reverse: create "system_state" table
DROP TABLE "system_state";
-- reverse: create "settings" table
DROP TABLE "settings";
