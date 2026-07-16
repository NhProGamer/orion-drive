-- +goose Up
CREATE TABLE `direct_links` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime,
    `updated_at` datetime,
    `token` text,
    `file_id` integer,
    `owner_id` integer,
    `downloads` integer
);
CREATE UNIQUE INDEX `idx_direct_links_token` ON `direct_links`(`token`);
CREATE INDEX `idx_direct_links_file_id` ON `direct_links`(`file_id`);
CREATE INDEX `idx_direct_links_owner_id` ON `direct_links`(`owner_id`);

-- +goose Down
DROP TABLE `direct_links`;
