ALTER TABLE groups ADD COLUMN IF NOT EXISTS subscription_concurrency integer NOT NULL DEFAULT 0 CHECK (subscription_concurrency BETWEEN 0 AND 100000);
ALTER TABLE user_subscriptions ADD COLUMN IF NOT EXISTS concurrency_limit integer NOT NULL DEFAULT 0 CHECK (concurrency_limit BETWEEN 0 AND 100000);
