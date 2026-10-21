-- Video workbench: durable projects, owned assets and transactional task holds.
CREATE TABLE IF NOT EXISTS video_studio_channels (
    group_id BIGINT PRIMARY KEY REFERENCES groups(id),
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    config JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS video_studio_projects (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    revision INTEGER NOT NULL DEFAULT 1,
    document JSONB NOT NULL,
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS video_studio_projects_owner ON video_studio_projects(user_id, updated_at DESC);
CREATE TABLE IF NOT EXISTS video_studio_assets (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    name TEXT NOT NULL,
    mime TEXT NOT NULL,
    content BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS video_studio_assets_owner ON video_studio_assets(user_id, created_at DESC);
CREATE TABLE IF NOT EXISTS video_studio_tasks (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
    group_id BIGINT NOT NULL REFERENCES groups(id),
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    project_id TEXT REFERENCES video_studio_projects(id),
    shot_id TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    request JSONB NOT NULL,
    pricing JSONB NOT NULL,
    upstream_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    result JSONB NOT NULL DEFAULT '{}',
    error_message TEXT NOT NULL DEFAULT '',
    hold_amount NUMERIC(20,8) NOT NULL CHECK (hold_amount >= 0),
    cost NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (cost >= 0),
    settled BOOLEAN NOT NULL DEFAULT FALSE,
    poll_after TIMESTAMPTZ NOT NULL DEFAULT now(),
    poll_deadline TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '24 hours'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS video_studio_tasks_owner ON video_studio_tasks(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS video_studio_tasks_poll ON video_studio_tasks(poll_after) WHERE NOT settled;
-- Reserves key quota as well as wallet funds; prevents parallel submissions
-- from bypassing a key's lifetime budget.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS video_reserved NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (video_reserved >= 0);
