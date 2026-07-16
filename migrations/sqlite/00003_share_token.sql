-- +goose Up
ALTER TABLE `shares` ADD COLUMN `token` text;
CREATE UNIQUE INDEX `idx_shares_token` ON `shares`(`token`);

-- +goose Down
DROP INDEX `idx_shares_token`;
ALTER TABLE `shares` DROP COLUMN `token`;
