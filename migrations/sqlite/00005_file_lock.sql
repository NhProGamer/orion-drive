-- +goose Up
-- A locked file records the user that locked it; while locked it cannot be
-- modified (renamed, moved, trashed, overwritten or version-changed).
ALTER TABLE `files` ADD COLUMN `lock_owner_id` integer;

-- +goose Down
ALTER TABLE `files` DROP COLUMN `lock_owner_id`;
