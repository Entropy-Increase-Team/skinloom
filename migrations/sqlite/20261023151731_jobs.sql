-- +goose Up
-- create "jobs" table
CREATE TABLE `jobs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `kind` text NOT NULL, `payload` text NOT NULL, `status` text NOT NULL DEFAULT ('pending'), `attempts` integer NOT NULL DEFAULT (0), `run_at` datetime NOT NULL, `locked_until` datetime NULL, `last_error` text NOT NULL DEFAULT (''), `created_at` datetime NOT NULL, `finished_at` datetime NULL);
-- create index "job_status_run_at" to table: "jobs"
CREATE INDEX `job_status_run_at` ON `jobs` (`status`, `run_at`);

-- +goose Down
-- reverse: create index "job_status_run_at" to table: "jobs"
DROP INDEX `job_status_run_at`;
-- reverse: create "jobs" table
DROP TABLE `jobs`;
