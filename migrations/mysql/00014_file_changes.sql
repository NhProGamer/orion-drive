-- +goose Up
-- Append-only change journal powering WebDAV sync-collection (RFC 6578): each
-- row is one change to a member (upsert or delete). The autoincrement id is the
-- monotonic sync token. Deletions are recorded as tombstones so a client syncing
-- after a purge still learns the member is gone.
CREATE TABLE `file_changes` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `user_id` bigint,
    `path` text,
    `is_dir` tinyint(1),
    `deleted` tinyint(1),
    `created_at` datetime,
    INDEX `idx_file_changes_user_id` (`user_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE `file_changes`;
