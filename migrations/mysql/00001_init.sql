-- +goose Up
-- Consolidated MySQL baseline: the full schema as of the 9 SQLite migrations
-- (00001..00009), collapsed into one file. MySQL support was added after those,
-- so no existing MySQL database needs the incremental history.
--
-- Notes: `groups` and `key` are reserved words (back-quoted throughout); columns
-- that back an index use VARCHAR rather than TEXT (InnoDB cannot index a bare
-- TEXT column); booleans are TINYINT(1); ids are BIGINT AUTO_INCREMENT.

CREATE TABLE `nodes` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `created_at` datetime,
    `updated_at` datetime,
    `name` text,
    `type` bigint,
    `status` bigint,
    `server` text,
    `slave_key` text,
    `weight` bigint,
    `settings` json
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `storage_policies` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `created_at` datetime,
    `updated_at` datetime,
    `name` text,
    `type` text,
    `server` text,
    `bucket_name` text,
    `base_path` text,
    `access_key` text,
    `secret_key` text,
    `node_id` bigint,
    `settings` json
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `groups` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `created_at` datetime,
    `updated_at` datetime,
    `name` text,
    `max_storage` bigint,
    `speed_limit` bigint,
    `permissions` json,
    `storage_policy_id` bigint,
    `settings` json,
    `sso_groups` text
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `users` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `created_at` datetime,
    `updated_at` datetime,
    `email` varchar(255),
    `subject` varchar(255),
    `nick` text,
    `avatar` text,
    `status` bigint,
    `storage_used` bigint,
    `group_id` bigint,
    `settings` json,
    `sso_groups` text,
    INDEX `idx_users_subject` (`subject`),
    UNIQUE INDEX `idx_users_email` (`email`),
    CONSTRAINT `fk_users_group` FOREIGN KEY (`group_id`) REFERENCES `groups`(`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `files` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `created_at` datetime,
    `updated_at` datetime,
    `name` varchar(255),
    `type` bigint,
    `owner_id` bigint,
    `parent_id` bigint,
    `primary_entity_id` bigint,
    `size` bigint,
    `starred` tinyint(1),
    `is_symbolic` tinyint(1),
    `storage_policy_id` bigint,
    `lock_owner_id` bigint,
    `props` json,
    `trashed_at` datetime,
    INDEX `idx_files_trashed_at` (`trashed_at`),
    INDEX `idx_files_parent_id` (`parent_id`),
    INDEX `idx_files_owner_id` (`owner_id`),
    INDEX `idx_files_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `entities` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `created_at` datetime,
    `updated_at` datetime,
    `type` bigint,
    `source` varchar(255),
    `size` bigint,
    `reference_count` bigint,
    `storage_policy_id` bigint,
    `upload_session_id` varchar(255),
    `created_by_id` bigint,
    `file_id` bigint,
    `props` json,
    INDEX `idx_entities_upload_session_id` (`upload_session_id`),
    INDEX `idx_entities_source` (`source`),
    INDEX `idx_entities_file_id` (`file_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `shares` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `created_at` datetime,
    `updated_at` datetime,
    `file_id` bigint,
    `user_id` bigint,
    `password` text,
    `token` varchar(255),
    `views` bigint,
    `downloads` bigint,
    `remain_downloads` bigint,
    `expires` datetime,
    `props` json,
    INDEX `idx_shares_user_id` (`user_id`),
    INDEX `idx_shares_file_id` (`file_id`),
    UNIQUE INDEX `idx_shares_token` (`token`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `direct_links` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `created_at` datetime,
    `updated_at` datetime,
    `token` varchar(255),
    `file_id` bigint,
    `owner_id` bigint,
    `downloads` bigint,
    UNIQUE INDEX `idx_direct_links_token` (`token`),
    INDEX `idx_direct_links_file_id` (`file_id`),
    INDEX `idx_direct_links_owner_id` (`owner_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `webdav_accounts` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `created_at` datetime,
    `updated_at` datetime,
    `user_id` bigint,
    `label` text,
    `username` varchar(255),
    `password_hash` text,
    `read_only` tinyint(1),
    `last_used_at` datetime,
    INDEX `idx_webdav_accounts_user_id` (`user_id`),
    UNIQUE INDEX `idx_webdav_accounts_username` (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `settings` (
    `key` varchar(255) PRIMARY KEY,
    `value` text
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Seed: default local storage policy and default group (mirrors 00002_seed).
INSERT INTO `storage_policies` (`id`, `name`, `type`, `base_path`)
VALUES (1, 'Default local', 'local', 'data/storage');

INSERT INTO `groups` (`id`, `name`, `max_storage`, `storage_policy_id`, `permissions`)
VALUES (1, 'Default', 53687091200, 1, '{"is_admin":true}');

-- +goose Down
DROP TABLE `settings`;
DROP TABLE `webdav_accounts`;
DROP TABLE `direct_links`;
DROP TABLE `shares`;
DROP TABLE `entities`;
DROP TABLE `files`;
DROP TABLE `users`;
DROP TABLE `groups`;
DROP TABLE `storage_policies`;
DROP TABLE `nodes`;
