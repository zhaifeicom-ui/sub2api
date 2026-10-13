-- Keep historical 6-sol runs intact; new checks target 6.1-sol instead.
ALTER TABLE channel_quality_test_configs DROP CONSTRAINT channel_quality_test_configs_models_check;
ALTER TABLE channel_quality_test_configs ALTER COLUMN models SET DEFAULT ARRAY['gpt-6-astra','gpt-6.1-sol']::TEXT[];
UPDATE channel_quality_test_configs
SET models=array_replace(models,'gpt-6-sol','gpt-6.1-sol'),version=version+1,updated_at=NOW()
WHERE 'gpt-6-sol'=ANY(models);
ALTER TABLE channel_quality_test_configs ADD CONSTRAINT channel_quality_test_configs_models_check
CHECK (cardinality(models) BETWEEN 1 AND 2 AND models <@ ARRAY['gpt-6-astra','gpt-6.1-sol']::TEXT[]);

ALTER TABLE channel_quality_test_runs DROP CONSTRAINT channel_quality_test_runs_model_check;
ALTER TABLE channel_quality_test_runs ADD CONSTRAINT channel_quality_test_runs_model_check
CHECK (model IN ('gpt-6-astra','gpt-6.1-sol','gpt-6-sol'));
-- Ten-minute job timeout plus a minute for finishing/persisting the result.
ALTER TABLE channel_quality_test_runs ALTER COLUMN deadline_at SET DEFAULT NOW()+INTERVAL '11 minutes';

ALTER TABLE channel_quality_test_schedule DROP CONSTRAINT channel_quality_test_schedule_model_check;
UPDATE channel_quality_test_schedule SET model='gpt-6.1-sol' WHERE model='gpt-6-sol';
ALTER TABLE channel_quality_test_schedule ADD CONSTRAINT channel_quality_test_schedule_model_check
CHECK (model IN ('gpt-6-astra','gpt-6.1-sol'));
