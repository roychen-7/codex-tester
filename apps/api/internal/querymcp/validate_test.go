package querymcp

import "testing"

func TestValidate(t *testing.T) {
	for _, query := range []string{
		"SELECT count(*) FROM analytics.users",
		"SELECT user_id,sum(pay_amount) FROM analytics.payment_orders WHERE status=$1 GROUP BY user_id",
		"SELECT date_trunc('day',paid_at),count(DISTINCT user_id) FROM analytics.payment_orders GROUP BY 1",
	} {
		var params []string
		if query == "SELECT user_id,sum(pay_amount) FROM analytics.payment_orders WHERE status=$1 GROUP BY user_id" {
			params = []string{"COMPLETED"}
		}

		if _, err := Validate(query, params); err != nil {
			t.Fatalf("valid query rejected: %s: %v", query, err)
		}
	}

	for _, query := range []string{
		"DELETE FROM analytics.users", "SELECT 1; SELECT 2", "SELECT * FROM public.syslog",
		"SELECT * FROM public.users", "SELECT * FROM analytics.unknown", "SELECT * FROM users",
		"SELECT pg_sleep(10)", "SELECT pg_catalog.set_config('statement_timeout','0',true)",
		"SELECT * FROM analytics.users FOR UPDATE", "SELECT * INTO x FROM analytics.users",
		"WITH d AS (DELETE FROM public.users RETURNING *) SELECT * FROM d",
		"WITH RECURSIVE x AS (SELECT 1 UNION ALL SELECT 1 FROM x) SELECT * FROM x",
		"SELECT * FROM generate_series(1,1000000000)", "SELECT public.sum(id) FROM analytics.users",
		"SELECT id::public.secret FROM analytics.users", "SELECT id OPERATOR(public.+) 1 FROM analytics.users",
		"SELECT * FROM analytics.users WHERE id=$2", "SELECT current_setting('agent.window_start')",
	} {
		if _, err := Validate(query, nil); err == nil {
			t.Fatalf("unsafe query accepted: %s", query)
		}
	}

	if _, err := Validate("SELECT count(*) FROM analytics.users", []string{"unused"}); err == nil {
		t.Fatal("unused parameter accepted")
	}
}
