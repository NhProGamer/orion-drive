-- +goose Up
-- Append-only change journal powering WebDAV sync-collection (RFC 6578): each
-- row is one change to a member (upsert or delete). The autoincrement id is the
-- monotonic sync token. Deletions are recorded as tombstones so a client syncing
-- after a purge still learns the member is gone.
CREATE TABLE file_changes (
    id bigserial PRIMARY KEY,
    user_id bigint,
    path text,
    is_dir boolean,
    deleted boolean,
    created_at timestamptz
);
CREATE INDEX idx_file_changes_user_id ON file_changes(user_id, id);

-- +goose Down
DROP TABLE file_changes;
