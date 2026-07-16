-- +goose Up
-- Consolidated PostgreSQL baseline: the full schema as of the 9 SQLite
-- migrations (00001..00009), collapsed into one file. PostgreSQL support was
-- added after those, so no existing PostgreSQL database needs the incremental
-- history — a single baseline is enough.

CREATE TABLE nodes (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    name text,
    type bigint,
    status bigint,
    server text,
    slave_key text,
    weight bigint,
    settings jsonb
);

CREATE TABLE storage_policies (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    name text,
    type text,
    server text,
    bucket_name text,
    base_path text,
    access_key text,
    secret_key text,
    node_id bigint,
    settings jsonb
);

CREATE TABLE groups (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    name text,
    max_storage bigint,
    speed_limit bigint,
    permissions jsonb,
    storage_policy_id bigint,
    settings jsonb,
    sso_groups text
);

CREATE TABLE users (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    email text,
    subject text,
    nick text,
    avatar text,
    status bigint,
    storage_used bigint,
    group_id bigint,
    settings jsonb,
    sso_groups text,
    CONSTRAINT fk_users_group FOREIGN KEY (group_id) REFERENCES groups(id)
);
CREATE INDEX idx_users_subject ON users(subject);
CREATE UNIQUE INDEX idx_users_email ON users(email);

CREATE TABLE files (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    name text,
    type bigint,
    owner_id bigint,
    parent_id bigint,
    primary_entity_id bigint,
    size bigint,
    starred boolean,
    is_symbolic boolean,
    storage_policy_id bigint,
    lock_owner_id bigint,
    props jsonb,
    trashed_at timestamptz
);
CREATE INDEX idx_files_trashed_at ON files(trashed_at);
CREATE INDEX idx_files_parent_id ON files(parent_id);
CREATE INDEX idx_files_owner_id ON files(owner_id);
CREATE INDEX idx_files_name ON files(name);

CREATE TABLE entities (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    type bigint,
    source text,
    size bigint,
    reference_count bigint,
    storage_policy_id bigint,
    upload_session_id text,
    created_by_id bigint,
    file_id bigint,
    props jsonb
);
CREATE INDEX idx_entities_upload_session_id ON entities(upload_session_id);
CREATE INDEX idx_entities_source ON entities(source);
CREATE INDEX idx_entities_file_id ON entities(file_id);

CREATE TABLE shares (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    file_id bigint,
    user_id bigint,
    password text,
    token text,
    views bigint,
    downloads bigint,
    remain_downloads bigint,
    expires timestamptz,
    props jsonb
);
CREATE INDEX idx_shares_user_id ON shares(user_id);
CREATE INDEX idx_shares_file_id ON shares(file_id);
CREATE UNIQUE INDEX idx_shares_token ON shares(token);

CREATE TABLE direct_links (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    token text,
    file_id bigint,
    owner_id bigint,
    downloads bigint
);
CREATE UNIQUE INDEX idx_direct_links_token ON direct_links(token);
CREATE INDEX idx_direct_links_file_id ON direct_links(file_id);
CREATE INDEX idx_direct_links_owner_id ON direct_links(owner_id);

CREATE TABLE webdav_accounts (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    user_id bigint,
    label text,
    username text,
    password_hash text,
    read_only boolean,
    last_used_at timestamptz
);
CREATE INDEX idx_webdav_accounts_user_id ON webdav_accounts(user_id);
CREATE UNIQUE INDEX idx_webdav_accounts_username ON webdav_accounts(username);

CREATE TABLE settings (
    key text PRIMARY KEY,
    value text
);

-- Seed: default local storage policy and default group (mirrors 00002_seed).
INSERT INTO storage_policies (id, name, type, base_path)
VALUES (1, 'Default local', 'local', 'data/storage');

INSERT INTO groups (id, name, max_storage, storage_policy_id, permissions)
VALUES (1, 'Default', 53687091200, 1, '{"is_admin":true}');

-- Explicit ids above do not advance the bigserial sequences; bump them so the
-- next auto-generated id does not collide with the seeded rows.
SELECT setval(pg_get_serial_sequence('storage_policies', 'id'), (SELECT MAX(id) FROM storage_policies));
SELECT setval(pg_get_serial_sequence('groups', 'id'), (SELECT MAX(id) FROM groups));

-- +goose Down
DROP TABLE settings;
DROP TABLE webdav_accounts;
DROP TABLE direct_links;
DROP TABLE shares;
DROP TABLE entities;
DROP TABLE files;
DROP TABLE users;
DROP TABLE groups;
DROP TABLE storage_policies;
DROP TABLE nodes;
