-- +goose Up
-- Personal access tokens: Bearer credentials for non-interactive clients
-- (native apps, the KDE KIO worker, sync clients). Only a hash is stored.
-- Indexed columns use VARCHAR (InnoDB cannot index a bare TEXT column).
CREATE TABLE `api_tokens` (
    `id` bigint AUTO_INCREMENT PRIMARY KEY,
    `created_at` datetime,
    `updated_at` datetime,
    `user_id` bigint,
    `label` text,
    `prefix` text,
    `token_hash` varchar(255),
    `read_only` tinyint(1),
    `expires_at` datetime,
    `last_used_at` datetime,
    INDEX `idx_api_tokens_user_id` (`user_id`),
    UNIQUE INDEX `idx_api_tokens_token_hash` (`token_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE `api_tokens`;
