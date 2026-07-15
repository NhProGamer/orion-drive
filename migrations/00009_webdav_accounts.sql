-- +goose Up
CREATE TABLE `webdav_accounts` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime,
    `updated_at` datetime,
    `user_id` integer,
    `label` text,
    `username` text,
    `password_hash` text,
    `read_only` numeric,
    `last_used_at` datetime
);
CREATE INDEX `idx_webdav_accounts_user_id` ON `webdav_accounts`(`user_id`);
CREATE UNIQUE INDEX `idx_webdav_accounts_username` ON `webdav_accounts`(`username`);

-- +goose Down
DROP TABLE `webdav_accounts`;
