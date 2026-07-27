-- +goose Up
-- Shares gain a permission level: read (view/download), write (full access) or
-- deposit (blind upload-only drop box). Existing shares default to read.
ALTER TABLE shares ADD COLUMN permission varchar(16) NOT NULL DEFAULT 'read';

-- +goose Down
ALTER TABLE shares DROP COLUMN permission;
