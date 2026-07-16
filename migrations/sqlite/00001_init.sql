-- +goose Up
CREATE TABLE `nodes` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime,
    `updated_at` datetime,
    `name` text,
    `type` integer,
    `status` integer,
    `server` text,
    `slave_key` text,
    `weight` integer,
    `settings` json
);

CREATE TABLE `storage_policies` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime,
    `updated_at` datetime,
    `name` text,
    `type` text,
    `server` text,
    `bucket_name` text,
    `base_path` text,
    `access_key` text,
    `secret_key` text,
    `node_id` integer,
    `settings` json
);

CREATE TABLE `groups` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime,
    `updated_at` datetime,
    `name` text,
    `max_storage` integer,
    `speed_limit` integer,
    `permissions` json,
    `storage_policy_id` integer,
    `settings` json
);

CREATE TABLE `users` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime,
    `updated_at` datetime,
    `email` text,
    `subject` text,
    `nick` text,
    `avatar` text,
    `status` integer,
    `storage_used` integer,
    `group_id` integer,
    `settings` json,
    CONSTRAINT `fk_users_group` FOREIGN KEY (`group_id`) REFERENCES `groups`(`id`)
);
CREATE INDEX `idx_users_subject` ON `users`(`subject`);
CREATE UNIQUE INDEX `idx_users_email` ON `users`(`email`);

CREATE TABLE `files` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime,
    `updated_at` datetime,
    `name` text,
    `type` integer,
    `owner_id` integer,
    `parent_id` integer,
    `primary_entity_id` integer,
    `size` integer,
    `starred` numeric,
    `is_symbolic` numeric,
    `storage_policy_id` integer,
    `props` json,
    `trashed_at` datetime
);
CREATE INDEX `idx_files_trashed_at` ON `files`(`trashed_at`);
CREATE INDEX `idx_files_parent_id` ON `files`(`parent_id`);
CREATE INDEX `idx_files_owner_id` ON `files`(`owner_id`);
CREATE INDEX `idx_files_name` ON `files`(`name`);

CREATE TABLE `entities` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime,
    `updated_at` datetime,
    `type` integer,
    `source` text,
    `size` integer,
    `reference_count` integer,
    `storage_policy_id` integer,
    `upload_session_id` text,
    `created_by_id` integer,
    `props` json
);
CREATE INDEX `idx_entities_upload_session_id` ON `entities`(`upload_session_id`);
CREATE INDEX `idx_entities_source` ON `entities`(`source`);

CREATE TABLE `shares` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime,
    `updated_at` datetime,
    `file_id` integer,
    `user_id` integer,
    `password` text,
    `views` integer,
    `downloads` integer,
    `remain_downloads` integer,
    `expires` datetime,
    `props` json
);
CREATE INDEX `idx_shares_user_id` ON `shares`(`user_id`);
CREATE INDEX `idx_shares_file_id` ON `shares`(`file_id`);

CREATE TABLE `settings` (
    `key` text,
    `value` text,
    PRIMARY KEY (`key`)
);

-- +goose Down
DROP TABLE `shares`;
DROP TABLE `entities`;
DROP TABLE `files`;
DROP TABLE `users`;
DROP TABLE `groups`;
DROP TABLE `storage_policies`;
DROP TABLE `nodes`;
DROP TABLE `settings`;
