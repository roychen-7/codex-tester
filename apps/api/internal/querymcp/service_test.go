package querymcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"gorm.io/gorm"
)

func fixture(t *testing.T) *Service {
	t.Helper()
	dsn := config.TestPostgresDSN()
	if dsn == "" {
		t.Skip("设置 TEST_POSTGRES_DSN 启用隔离 PostgreSQL 测试")
	}

	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("仅允许 _test 数据库")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		s.Close()
	})
	return s
}

func input() PrepareInput {
	return PrepareInput{SQL: "SELECT user_id, sum(pay_amount) AS amount FROM analytics.payment_orders WHERE status = $1 GROUP BY user_id ORDER BY user_id", Parameters: []string{"COMPLETED"}, Purpose: "测试九月充值", WindowStart: "2026-09-01T00:00:00Z", WindowEnd: "2026-10-01T00:00:00Z"}
}

func TestPostgresQuery(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	prepared, err := s.Prepare(ctx, "user-1:run-1", input())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Execute(ctx, "user-2:run-1", prepared.QueryID); err == nil {
		t.Fatal("cross-owner query executed")
	}

	result, err := s.Execute(ctx, "user-1:run-1", prepared.QueryID)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Rows) != 2 || result.Rows[0][1] != "120.00" || result.Rows[1][1] != "50.00" {
		t.Fatalf("wrong aggregate: %#v", result.Rows)
	}

	if _, err := s.Execute(ctx, "user-1:run-1", prepared.QueryID); err == nil {
		t.Fatal("ticket replay accepted")
	}

	in := input()
	in.SQL = "SELECT count(*) AS calls FROM analytics.usage_logs"
	in.Parameters = nil
	p, err := s.Prepare(ctx, "owner", in)
	if err != nil {
		t.Fatal(err)
	}

	r, err := s.Execute(ctx, "owner", p.QueryID)
	if err != nil || len(r.Rows) != 1 || r.Rows[0][0] != "2" {
		t.Fatalf("time window not enforced: %+v %v", r, err)
	}

	in.Parameters = []string{"COMPLETED' OR true --"}
	in.SQL = "SELECT count(*) FROM analytics.payment_orders WHERE status=$1"
	p, err = s.Prepare(ctx, "owner", in)
	if err != nil {
		t.Fatal(err)
	}

	r, err = s.Execute(ctx, "owner", p.QueryID)
	if err != nil || r.Rows[0][0] != "0" {
		t.Fatal("parameter injection", r, err)
	}

	in = input()
	in.WindowEnd = "2027-01-01T00:00:00Z"
	if _, err := s.Prepare(ctx, "owner", in); err == nil {
		t.Fatal("unbounded date window accepted")
	}
	// Database role independently rejects raw-table reads and writes even without the AST validator.
	for _, q := range []string{"SELECT * FROM public.users", "SELECT * FROM public.syslog", "DELETE FROM analytics.users", "CREATE TABLE public.forbidden(id int)"} {
		if err := s.db.Exec(q).Error; err == nil {
			t.Fatal("DB role allowed forbidden operation", q)
		}
	}
	// Only the harmless sleep fixture bypasses policy here, to verify database timeout/cancellation.
	s.timeout = 80 * time.Millisecond
	if err := s.transaction(ctx, input(), func(tx *gorm.DB) error {
		return tx.Exec("SELECT pg_sleep(1)").Error
	}); err == nil {
		t.Fatal("statement deadline ignored")
	}

	s.timeout = time.Second
	cancelled, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(30*time.Millisecond, cancel)
	defer timer.Stop()
	defer cancel()
	start := time.Now()
	err = s.transaction(cancelled, input(), func(tx *gorm.DB) error {
		return tx.Exec("SELECT pg_sleep(1)").Error
	})
	if err == nil || time.Since(start) > 500*time.Millisecond {
		t.Fatal("request cancellation did not stop query")
	}
}

func TestResultBounds(t *testing.T) {
	s := fixture(t)
	in := input()
	in.Parameters = nil
	in.SQL = "SELECT a.id FROM analytics.users a CROSS JOIN analytics.users b CROSS JOIN analytics.users c CROSS JOIN analytics.users d CROSS JOIN analytics.users e"
	p, err := s.Prepare(context.Background(), "owner", in)
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.Execute(context.Background(), "owner", p.QueryID)
	if err != nil || len(result.Rows) != 200 || !result.Truncated {
		t.Fatal("row limit not enforced", len(result.Rows), err)
	}

	in.SQL = "SELECT " + strings.TrimSuffix(strings.Repeat("$1::text,", 150), ",")
	in.Parameters = []string{strings.Repeat("a", 2000)}
	p, err = s.Prepare(context.Background(), "owner", in)
	if err != nil {
		t.Fatal(err)
	}

	result, err = s.Execute(context.Background(), "owner", p.QueryID)
	if err != nil || len(result.Rows) != 0 || !result.Truncated {
		t.Fatal("byte limit not enforced", err)
	}
}

type authenticatedTransport struct {
	base  http.RoundTripper
	owner string
}

func (a authenticatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer test-only-token-at-least-32-characters")
	clone.Header.Set("X-Agent-Subject", a.owner)
	return a.base.RoundTrip(clone)
}

func TestMCPProtocol(t *testing.T) {
	s := fixture(t)
	server := httptest.NewServer(NewHandler(s, "test-only-token-at-least-32-characters"))
	defer server.Close()
	response, err := http.Post(server.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("unauthenticated MCP accepted")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "integration", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: authenticatedTransport{http.DefaultTransport, "user:task"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	defer session.Close()
	list, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil || len(list.Tools) != 3 {
		t.Fatal("tools not discoverable", err)
	}

	describe, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "describe_dataset", Arguments: DescribeInput{Table: "payment_orders"}})
	if err != nil || describe.IsError {
		t.Fatal("describe failed", describe, err)
	}

	prepared, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "prepare_query", Arguments: input()})
	if err != nil || prepared.IsError {
		t.Fatal("prepare failed", prepared, err)
	}

	raw, err := json.Marshal(prepared.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}

	var p Prepared
	if json.Unmarshal(raw, &p) != nil || p.QueryID == "" {
		t.Fatalf("missing ticket: %s", raw)
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "execute_query", Arguments: ExecuteInput{QueryID: p.QueryID}})
	if err != nil || result.IsError {
		t.Fatal("execution failed", result, err)
	}
}

func TestTicketConcurrencyAndExpiry(t *testing.T) {
	s := fixture(t)
	p, err := s.Prepare(context.Background(), "owner", input())
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	successes := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Execute(context.Background(), "owner", p.QueryID)
			successes <- err == nil
		}()
	}

	wg.Wait()
	close(successes)
	count := 0
	for ok := range successes {
		if ok {
			count++
		}
	}

	if count != 1 {
		t.Fatal("concurrent ticket executed more than once")
	}

	p, err = s.Prepare(context.Background(), "owner", input())
	if err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	item := s.tickets[p.QueryID]
	item.expires = time.Now().Add(-time.Second)
	s.tickets[p.QueryID] = item
	s.mu.Unlock()
	if _, err := s.Execute(context.Background(), "owner", p.QueryID); err == nil {
		t.Fatal("expired ticket accepted")
	}
}
