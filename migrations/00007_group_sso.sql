-- +goose Up
-- Comma-separated SSO group/role names that map onto this OrionDrive group.
ALTER TABLE `groups` ADD COLUMN `sso_groups` text;

-- +goose Down
ALTER TABLE `groups` DROP COLUMN `sso_groups`;
