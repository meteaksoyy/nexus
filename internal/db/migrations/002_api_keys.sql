-- +goose Up
CREATE TABLE api_keys (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_hash   TEXT UNIQUE NOT NULL,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used  TIMESTAMPTZ
);

CREATE INDEX idx_api_keys_user_id ON api_keys(user_id);

-- +goose Down
DROP TABLE api_keys;
