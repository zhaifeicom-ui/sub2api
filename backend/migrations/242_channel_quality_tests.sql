-- Visual model-quality checks belong to a configured channel monitor. Keep the
-- former group-scoped tables intact so existing configurations and results are
-- preserved; this new schema uses each monitor's own endpoint and encrypted key.
CREATE TABLE channel_quality_test_configs (
    monitor_id BIGINT PRIMARY KEY REFERENCES channel_monitors(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    models TEXT[] NOT NULL DEFAULT ARRAY['gpt-6-astra','gpt-6-sol']::TEXT[],
    customer_access TEXT NOT NULL DEFAULT 'hidden' CHECK (customer_access IN ('hidden','view','run')),
    cooldown_seconds INTEGER NOT NULL DEFAULT 600 CHECK (cooldown_seconds BETWEEN 300 AND 86400),
    auto_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    auto_interval_seconds INTEGER NOT NULL DEFAULT 600 CHECK (auto_interval_seconds BETWEEN 600 AND 31536000),
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (cardinality(models) BETWEEN 1 AND 2 AND models <@ ARRAY['gpt-6-astra','gpt-6-sol']::TEXT[])
);

-- Run artifacts survive config/channel removal and retain the channel's name.
CREATE TABLE channel_quality_test_runs (
    id BIGSERIAL PRIMARY KEY,
    monitor_id BIGINT NOT NULL,
    monitor_name TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL CHECK (model IN ('gpt-6-astra','gpt-6-sol')),
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
CREATE UNIQUE INDEX channel_quality_one_running ON channel_quality_test_runs ((TRUE)) WHERE status = 'running';
CREATE INDEX channel_quality_history ON channel_quality_test_runs (monitor_id, model, id DESC);

CREATE TABLE channel_quality_test_schedule (
    monitor_id BIGINT NOT NULL REFERENCES channel_quality_test_configs(monitor_id) ON DELETE CASCADE,
    model TEXT NOT NULL CHECK (model IN ('gpt-6-astra','gpt-6-sol')),
    next_run_at TIMESTAMPTZ NOT NULL,
    last_attempt_at TIMESTAMPTZ,
    last_outcome TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(monitor_id, model)
);
CREATE INDEX channel_quality_schedule_due ON channel_quality_test_schedule(next_run_at);
