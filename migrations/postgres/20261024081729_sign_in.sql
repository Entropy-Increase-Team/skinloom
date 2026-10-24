-- +goose Up
-- modify "users" table
ALTER TABLE "users" ADD COLUMN "last_sign_at" timestamptz NULL;

-- +goose Down
-- reverse: modify "users" table
ALTER TABLE "users" DROP COLUMN "last_sign_at";
