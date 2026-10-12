CREATE TABLE managed_prompt_policy_events (
 id BIGSERIAL PRIMARY KEY,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 request_id TEXT NOT NULL,
 group_id BIGINT NOT NULL,
 config_version BIGINT NOT NULL,
 status TEXT NOT NULL,
 transport_status TEXT NOT NULL DEFAULT '',
 protocol TEXT NOT NULL,
 rule_ids JSONB NOT NULL DEFAULT '[]',
 input_bytes_before BIGINT NOT NULL DEFAULT 0,
 input_bytes_after BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX managed_prompt_policy_events_recent ON managed_prompt_policy_events(created_at DESC);
CREATE INDEX managed_prompt_policy_events_request ON managed_prompt_policy_events(request_id);
