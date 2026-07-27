-- +goose Up
-- Personal access tokens: Bearer credentials for non-interactive clients
-- (native apps, the KDE KIO worker, sync clients). Only a hash is stored.
CREATE TABLE api_tokens (
    id bigserial PRIMARY KEY,
    created_at timestamptz,
    updated_at timestamptz,
    user_id bigint,
    label text,
    prefix text,
    token_hash text,
    read_only boolean,
    expires_at timestamptz,
    last_used_at timestamptz
);
CREATE INDEX idx_api_tokens_user_id ON api_tokens(user_id);
CREATE UNIQUE INDEX idx_api_tokens_token_hash ON api_tokens(token_hash);

-- +goose Down
DROP TABLE api_tokens;
