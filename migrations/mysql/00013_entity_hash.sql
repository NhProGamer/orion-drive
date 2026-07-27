-- +goose Up
-- Content hash (SHA-256, hex, 64 chars) of a stored object's plaintext. Powers
-- strong WebDAV ETags and content-addressed deduplication. VARCHAR so it can be
-- indexed (InnoDB cannot index a bare TEXT column).
ALTER TABLE `entities` ADD COLUMN `hash` varchar(64);
CREATE INDEX `idx_entities_hash` ON `entities`(`hash`);

-- +goose Down
DROP INDEX `idx_entities_hash` ON `entities`;
ALTER TABLE `entities` DROP COLUMN `hash`;
