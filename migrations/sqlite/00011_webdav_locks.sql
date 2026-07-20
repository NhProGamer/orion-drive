-- +goose Up
-- Persisted WebDAV locks (RFC 4918 Class 2): survive restarts and are shared
-- across cluster nodes, replacing the in-memory lock system.
CREATE TABLE `webdav_locks` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime,
    `updated_at` datetime,
    `token` text,
    `user_id` integer,
    `path` text,
    `depth` integer,
    `owner` text,
    `expires` datetime
);
CREATE UNIQUE INDEX `idx_webdav_locks_token` ON `webdav_locks`(`token`);
CREATE INDEX `idx_webdav_locks_user_id` ON `webdav_locks`(`user_id`);
CREATE INDEX `idx_webdav_locks_path` ON `webdav_locks`(`path`);
CREATE INDEX `idx_webdav_locks_expires` ON `webdav_locks`(`expires`);

-- +goose Down
DROP TABLE `webdav_locks`;
