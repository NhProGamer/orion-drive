-- +goose Up
-- The user's SSO groups/roles from their last login (for admin resolution).
ALTER TABLE `users` ADD COLUMN `sso_groups` text;

-- +goose Down
ALTER TABLE `users` DROP COLUMN `sso_groups`;
