-- Opt-in, group-scoped visual quality tests. No existing account/billing data is changed.
CREATE TABLE group_quality_test_configs (
    group_id BIGINT PRIMARY KEY REFERENCES groups(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    models TEXT[] NOT NULL DEFAULT ARRAY['gpt-6-astra','gpt-6-sol']::TEXT[],
    customer_access TEXT NOT NULL DEFAULT 'hidden' CHECK (customer_access IN ('hidden','view','run')),
    api_mode TEXT NOT NULL DEFAULT 'responses' CHECK (api_mode IN ('responses','chat_completions')),
    account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    cooldown_seconds INTEGER NOT NULL DEFAULT 600 CHECK (cooldown_seconds BETWEEN 300 AND 86400),
    auto_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    auto_interval_seconds INTEGER NOT NULL DEFAULT 600 CHECK (auto_interval_seconds BETWEEN 600 AND 31536000),
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (cardinality(models) BETWEEN 1 AND 2 AND models <@ ARRAY['gpt-6-astra','gpt-6-sol']::TEXT[])
);
CREATE TABLE group_quality_test_runs (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    model TEXT NOT NULL CHECK (model IN ('gpt-6-astra','gpt-6-sol')),
    account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    upstream_model TEXT NOT NULL,
    prompt_version TEXT NOT NULL DEFAULT 'pelican-animation-v1',
    api_mode TEXT NOT NULL,
    requested_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    automatic BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running','completed','failed')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deadline_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '6 minutes',
    finished_at TIMESTAMPTZ,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    response_length INTEGER NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    source TEXT NOT NULL DEFAULT '' CHECK (octet_length(source) <= 524288),
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT ''
);
-- Across replicas: one running test globally, without holding a DB connection for inference.
CREATE UNIQUE INDEX group_quality_one_running ON group_quality_test_runs ((TRUE)) WHERE status = 'running';
CREATE INDEX group_quality_history ON group_quality_test_runs (group_id, model, id DESC);
CREATE TABLE group_quality_test_schedule (
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    model TEXT NOT NULL CHECK (model IN ('gpt-6-astra','gpt-6-sol')),
    next_run_at TIMESTAMPTZ NOT NULL,
    last_attempt_at TIMESTAMPTZ,
    last_outcome TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(group_id,model)
);
CREATE INDEX group_quality_schedule_due ON group_quality_test_schedule(next_run_at);
