package querymcp

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/roychen-7/codex-tester/apps/api/internal/config"
)

func TestCompileLive(t *testing.T) {
	in := input()
	in.SQL = "SELECT analytics.usage_logs.user_id FROM analytics.usage_logs WHERE EXISTS (SELECT 1 FROM analytics.payment_orders p WHERE p.user_id=analytics.usage_logs.user_id AND p.status=$1)"
	sql, params, err := compileLive(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(params) != 3 || params[1] != in.WindowStart || !strings.Contains(sql, "public.usage_logs") || !strings.Contains(sql, "public.payment_orders") || strings.Count(sql, "$2") != 2 || strings.Contains(sql, "analytics.") {
		t.Fatalf("missing nested boundary: %s", sql)
	}
	for _, q := range []string{"SELECT * FROM public.users", "DELETE FROM analytics.users", "SELECT pg_read_file('/etc/passwd')", "SELECT * FROM analytics.syslog", "SELECT * INTO temp_copy FROM analytics.users"} {
		in.SQL, in.Parameters = q, nil
		if _, _, err := compileLive(in); err == nil {
			t.Fatalf("unsafe SQL accepted: %s", q)
		}
	}
}

func liveFixture(t *testing.T) *Service {
	t.Helper()
	dsn := config.TestPostgresDSN()
	if dsn == "" {
		t.Skip("设置 TEST_POSTGRES_DSN 启用隔离 PostgreSQL 测试")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("仅允许 _test 数据库")
	}
	u.User = url.UserPassword("live_analyst", "live-test-only")
	s, err := OpenWithSource(context.Background(), u.String(), SourceLive)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestLiveProjectionAndBoundaries(t *testing.T) {
	s := liveFixture(t)
	ctx := context.Background()
	for _, tc := range []struct{ sql, expected string }{
		{"SELECT count(*) FROM analytics.usage_logs", "2"},
		{"SELECT count(*) FROM analytics.usage_logs l WHERE true OR l.created_at < '2000-01-01'", "2"},
		{"SELECT count(*) FROM (SELECT * FROM analytics.usage_logs) x", "2"},
		{"SELECT count(*) FROM analytics.usage_logs a JOIN analytics.usage_logs b ON a.id=b.id", "2"},
		{"SELECT count(*) FROM analytics.users u WHERE EXISTS (SELECT 1 FROM analytics.usage_logs l WHERE l.user_id=u.id)", "2"},
		{"SELECT count(*) FROM analytics.users", "3"},
		{"SELECT sum(pay_amount) FROM analytics.payment_orders", "170.00"},
	} {
		in := input()
		in.SQL, in.Parameters = tc.sql, nil
		p, err := s.Prepare(ctx, "owner", in)
		if err != nil {
			t.Fatalf("%s: %v", tc.sql, err)
		}
		r, err := s.Execute(ctx, "owner", p.QueryID)
		if err != nil || len(r.Rows) != 1 || r.Rows[0][0] != tc.expected || r.DataSource != SourceLive || p.DataSource != SourceLive || r.ExecutedSQL == "" {
			t.Fatalf("wrong live result %s: %+v %v", tc.sql, r, err)
		}
	}
	for _, q := range []string{
		"SELECT password_hash FROM analytics.users",
		"SELECT u.password_hash FROM analytics.users u",
		"SELECT x.password_hash FROM (SELECT * FROM analytics.users) x",
		"SELECT u.ctid FROM analytics.users u",
		"SELECT * FROM public.users",
		"SELECT * FROM analytics.users FOR UPDATE",
		"UPDATE public.users SET balance=0",
	} {
		in := input()
		in.SQL, in.Parameters = q, nil
		if _, err := s.Prepare(ctx, "owner", in); err == nil {
			t.Fatalf("forbidden access accepted: %s", q)
		}
	}
	// A whole-row value must contain only the projected fields, never base-row secrets.
	in := input()
	in.SQL, in.Parameters = "SELECT u FROM analytics.users u LIMIT 1", nil
	p, err := s.Prepare(ctx, "owner", in)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Execute(ctx, "owner", p.QueryID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(r.Rows)
	if strings.Contains(string(encoded), "fixture-secret") {
		t.Fatal("whole-row reference leaked excluded fields")
	}
	description, err := s.Describe(ctx, DescribeInput{Table: "users"})
	if err != nil || description.DataSource != SourceLive {
		t.Fatal("missing live description", err)
	}
	emailAdvertised := false
	for _, c := range description.Columns {
		if c.Name == "email" {
			emailAdvertised = true
		}
		if c.Name == "password_hash" {
			t.Fatal("sensitive field advertised")
		}
	}
	if !emailAdvertised {
		t.Fatal("email missing from live dataset description")
	}
}

func TestLiveEmailSelectionAndFilter(t *testing.T) {
	s := liveFixture(t)
	ctx := context.Background()
	for _, email := range []string{"synthetic-1@example.invalid", "missing@example.invalid", "' OR true --"} {
		in := input()
		in.SQL = "SELECT u.id, u.email FROM analytics.users u WHERE u.email = $1"
		in.Parameters = []string{email}
		prepared, err := s.Prepare(ctx, "email-test", in)
		if err != nil {
			t.Fatal(err)
		}

		result, err := s.Execute(ctx, "email-test", prepared.QueryID)
		if err != nil {
			t.Fatal(err)
		}
		if email == "synthetic-1@example.invalid" {
			if len(result.Rows) != 1 || result.Rows[0][0] != "1" || result.Rows[0][1] != email {
				t.Fatal("email selection or filtering failed")
			}
		} else if len(result.Rows) != 0 {
			t.Fatal("unmatched or SQL-like email bypassed filtering")
		}
	}
}
