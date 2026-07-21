-- +goose Up
-- Personal access tokens: Bearer credentials for non-interactive clients
-- (native apps, the KDE KIO worker, sync clients). Only a hash is stored.
CREATE TABLE `api_tokens` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime,
    `updated_at` datetime,
    `user_id` integer,
    `label` text,
    `prefix` text,
    `token_hash` text,
    `read_only` numeric,
    `expires_at` datetime,
    `last_used_at` datetime
);
CREATE INDEX `idx_api_tokens_user_id` ON `api_tokens`(`user_id`);
CREATE UNIQUE INDEX `idx_api_tokens_token_hash` ON `api_tokens`(`token_hash`);

-- +goose Down
DROP TABLE `api_tokens`;
