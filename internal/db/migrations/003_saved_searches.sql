-- +goose Up
CREATE TABLE saved_searches (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    query      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_saved_searches_user_id ON saved_searches(user_id);

-- +goose Down
DROP TABLE saved_searches;
