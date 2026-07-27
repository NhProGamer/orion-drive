-- +goose Up
-- Content hash (SHA-256, hex) of a stored object's plaintext. Powers strong
-- WebDAV ETags and content-addressed deduplication.
ALTER TABLE entities ADD COLUMN hash text;
CREATE INDEX idx_entities_hash ON entities(hash);

-- +goose Down
DROP INDEX idx_entities_hash;
ALTER TABLE entities DROP COLUMN hash;
