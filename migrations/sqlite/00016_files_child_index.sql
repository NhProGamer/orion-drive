-- +goose Up
-- Composite index for the hottest file-tree query, FindChildByName
-- (WHERE owner_id = ? AND parent_id = ? AND name = ?), used on every navigation,
-- upload, rename and move. The existing single-column indexes cannot serve it
-- from one index; this covering index does.
CREATE INDEX `idx_files_owner_parent_name` ON `files`(`owner_id`, `parent_id`, `name`);

-- +goose Down
DROP INDEX `idx_files_owner_parent_name`;
