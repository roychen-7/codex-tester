-- Synthetic, reduced projection of sub2api v0.2.7. Never run on a business database.
REVOKE ALL ON DATABASE sub2api_test FROM PUBLIC;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
CREATE ROLE analyst LOGIN PASSWORD 'analyst-test-only' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
GRANT CONNECT ON DATABASE sub2api_test TO analyst;
ALTER ROLE analyst SET default_transaction_read_only = on;
ALTER ROLE analyst SET statement_timeout = '10s';
ALTER ROLE analyst SET lock_timeout = '1s';
ALTER ROLE analyst SET search_path = pg_catalog;
CREATE TABLE public.users (id bigint PRIMARY KEY, email text, created_at timestamptz NOT NULL, deleted_at timestamptz);
CREATE TABLE public.payment_orders (id bigint PRIMARY KEY, user_id bigint NOT NULL REFERENCES public.users(id), amount numeric(20,2), pay_amount numeric(20,2), refund_amount numeric(20,2), status varchar(30), order_type varchar(20), paid_at timestamptz, created_at timestamptz NOT NULL);
CREATE TABLE public.usage_logs (id bigint PRIMARY KEY, user_id bigint NOT NULL REFERENCES public.users(id), model varchar(100), input_tokens integer, output_tokens integer, total_cost numeric(20,10), actual_cost numeric(20,10), created_at timestamptz NOT NULL);
CREATE TABLE public.syslog (id bigint, content text);
CREATE INDEX ON public.payment_orders(paid_at);
CREATE INDEX ON public.usage_logs(created_at);
CREATE INDEX ON public.usage_logs(user_id, created_at);
INSERT INTO public.users VALUES (1,'synthetic-1@example.invalid','2026-08-01',NULL),(2,'synthetic-2@example.invalid','2026-08-02',NULL),(3,'synthetic-3@example.invalid','2026-08-03',NULL);
INSERT INTO public.payment_orders VALUES
(1,1,100,100,0,'COMPLETED','balance','2026-09-01T01:00:00Z','2026-09-01T00:59:00Z'),
(2,2,50,50,0,'COMPLETED','balance','2026-09-01T02:00:00Z','2026-09-01T01:59:00Z'),
(3,1,20,20,0,'COMPLETED','balance','2026-09-02T01:00:00Z','2026-09-02T00:59:00Z'),
(4,3,90,90,0,'PENDING','balance',NULL,'2026-09-02T01:00:00Z');
INSERT INTO public.usage_logs VALUES
(1,1,'synthetic-model',100,50,0.01,0.008,'2026-09-01T03:00:00Z'),
(2,2,'synthetic-model',200,50,0.02,0.016,'2026-09-02T03:00:00Z'),
(3,1,'synthetic-model',300,50,0.03,0.024,'2026-10-01T03:00:00Z');
CREATE SCHEMA analytics;
GRANT USAGE ON SCHEMA analytics TO analyst;
-- Credentials, email and IP are never exposed to the query role.
CREATE VIEW analytics.users WITH (security_barrier=true) AS SELECT id,created_at,deleted_at FROM public.users;
-- Bounds are set by the service inside each read-only transaction. Missing bounds fail closed.
CREATE VIEW analytics.payment_orders WITH (security_barrier=true) AS
 SELECT id,user_id,amount,pay_amount,refund_amount,status,order_type,paid_at,created_at FROM public.payment_orders
 WHERE paid_at >= current_setting('agent.window_start')::timestamptz AND paid_at < current_setting('agent.window_end')::timestamptz;
CREATE VIEW analytics.usage_logs WITH (security_barrier=true) AS
 SELECT id,user_id,model,input_tokens,output_tokens,total_cost,actual_cost,created_at FROM public.usage_logs
 WHERE created_at >= current_setting('agent.window_start')::timestamptz AND created_at < current_setting('agent.window_end')::timestamptz;
GRANT SELECT ON analytics.users,analytics.payment_orders,analytics.usage_logs TO analyst;
ANALYZE;
