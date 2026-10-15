-- Isolated test database only. Never execute against a business database.
DO $$ BEGIN
 IF current_database() <> 'sub2api_test' THEN RAISE EXCEPTION 'test database only'; END IF;
END $$;
ALTER TABLE public.users ADD COLUMN IF NOT EXISTS role varchar(20) DEFAULT 'user', ADD COLUMN IF NOT EXISTS status varchar(20) DEFAULT 'active', ADD COLUMN IF NOT EXISTS balance numeric(20,8) DEFAULT 0, ADD COLUMN IF NOT EXISTS total_recharged numeric(20,8) DEFAULT 0, ADD COLUMN IF NOT EXISTS signup_source varchar(20) DEFAULT 'email', ADD COLUMN IF NOT EXISTS last_login_at timestamptz, ADD COLUMN IF NOT EXISTS last_active_at timestamptz, ADD COLUMN IF NOT EXISTS password_hash text DEFAULT 'fixture-secret-never-expose';
ALTER TABLE public.payment_orders ADD COLUMN IF NOT EXISTS payment_type varchar(30) DEFAULT 'synthetic', ADD COLUMN IF NOT EXISTS completed_at timestamptz, ADD COLUMN IF NOT EXISTS refund_at timestamptz;
ALTER TABLE public.usage_logs ADD COLUMN IF NOT EXISTS requested_model varchar(100), ADD COLUMN IF NOT EXISTS upstream_model varchar(100), ADD COLUMN IF NOT EXISTS cache_creation_tokens integer DEFAULT 0, ADD COLUMN IF NOT EXISTS cache_read_tokens integer DEFAULT 0, ADD COLUMN IF NOT EXISTS billing_type smallint DEFAULT 0, ADD COLUMN IF NOT EXISTS duration_ms integer, ADD COLUMN IF NOT EXISTS first_token_ms integer;
DO $$ BEGIN
 IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='live_analyst') THEN
  CREATE ROLE live_analyst LOGIN PASSWORD 'live-test-only' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
 END IF;
END $$;
GRANT CONNECT ON DATABASE sub2api_test TO live_analyst;
GRANT USAGE ON SCHEMA public TO live_analyst;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO live_analyst;
ALTER ROLE live_analyst SET default_transaction_read_only = on;
