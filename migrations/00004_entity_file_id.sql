-- +goose Up
-- Associate version entities with their file so a file can keep a version history.
ALTER TABLE `entities` ADD COLUMN `file_id` integer;
CREATE INDEX `idx_entities_file_id` ON `entities`(`file_id`);

-- +goose Down
DROP INDEX `idx_entities_file_id`;
ALTER TABLE `entities` DROP COLUMN `file_id`;
