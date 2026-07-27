-- +goose Up
-- Persisted WebDAV locks (RFC 4918 Class 2): survive restarts and are shared
-- across cluster nodes, replacing the in-memory lock system.
CREATE TABLE webdav_locks (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    token text,
    user_id bigint,
    path text,
    depth bigint,
    owner text,
    expires timestamptz
);
CREATE UNIQUE INDEX idx_webdav_locks_token ON webdav_locks(token);
CREATE INDEX idx_webdav_locks_user_id ON webdav_locks(user_id);
CREATE INDEX idx_webdav_locks_path ON webdav_locks(path);
CREATE INDEX idx_webdav_locks_expires ON webdav_locks(expires);

-- +goose Down
DROP TABLE webdav_locks;
