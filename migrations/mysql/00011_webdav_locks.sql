-- +goose Up
-- Persisted WebDAV locks (RFC 4918 Class 2): survive restarts and are shared
-- across cluster nodes, replacing the in-memory lock system. Indexed columns use
-- VARCHAR (InnoDB cannot index a bare TEXT column).
CREATE TABLE `webdav_locks` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `created_at` datetime,
    `updated_at` datetime,
    `token` varchar(255),
    `user_id` bigint,
    `path` varchar(768),
    `depth` bigint,
    `owner` text,
    `expires` datetime,
    UNIQUE INDEX `idx_webdav_locks_token` (`token`),
    INDEX `idx_webdav_locks_user_id` (`user_id`),
    INDEX `idx_webdav_locks_path` (`path`),
    INDEX `idx_webdav_locks_expires` (`expires`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE `webdav_locks`;
