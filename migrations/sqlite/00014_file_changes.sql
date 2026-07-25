-- +goose Up
-- Append-only change journal powering WebDAV sync-collection (RFC 6578): each
-- row is one change to a member (upsert or delete). The autoincrement id is the
-- monotonic sync token. Deletions are recorded here as tombstones so a client
-- syncing after a purge still learns the member is gone.
CREATE TABLE `file_changes` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `user_id` integer,
    `path` text,
    `is_dir` numeric,
    `deleted` numeric,
    `created_at` datetime
);
CREATE INDEX `idx_file_changes_user_id` ON `file_changes`(`user_id`, `id`);

-- +goose Down
DROP TABLE `file_changes`;
