-- +goose Up
-- create "jobs" table
CREATE TABLE `jobs` (`id` bigint NOT NULL AUTO_INCREMENT, `kind` varchar(64) NOT NULL, `payload` longtext NOT NULL, `status` enum('pending','done','failed') NOT NULL DEFAULT "pending", `attempts` bigint NOT NULL DEFAULT 0, `run_at` datetime(6) NOT NULL, `locked_until` datetime(6) NULL, `last_error` varchar(1024) NOT NULL DEFAULT "", `created_at` datetime(6) NOT NULL, `finished_at` datetime(6) NULL, PRIMARY KEY (`id`), INDEX `job_status_run_at` (`status`, `run_at`)) CHARSET utf8mb4 COLLATE utf8mb4_bin;

-- +goose Down
-- reverse: create "jobs" table
DROP TABLE `jobs`;
