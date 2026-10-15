package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
)

func fixtureWeb(t *testing.T) *webService {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("missing auth")
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{"title": "Official pricing", "url": "https://example.com/pricing", "content": "search snippet"}, {"title": "private", "url": "https://127.0.0.1/secret", "content": "secret"}}})
		case "/extract":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{{"url": "https://example.com/pricing", "raw_content": strings.Repeat("价", 6100)}}})
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	service := newWebService("test-only")
	service.endpoint = server.URL
	service.resolve = func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
		if host == "example.com" {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}

		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}

	return service
}

func TestWebMCPAndBudgets(t *testing.T) {
	service := fixtureWeb(t)
	ctx := context.Background()
	remote, err := connectWebService(ctx, service)
	if err != nil {
		t.Fatal(err)
	}

	defer remote.Close()
	if len(remote.Definitions()) != 2 {
		t.Fatal("missing MCP tools")
	}

	if _, err := remote.Call(ctx, "read_webpage", raw(WebReadInput{URL: "https://example.com/pricing"})); err == nil {
		t.Fatal("unsearched URL accepted")
	}

	result, err := remote.Call(ctx, "search_public_web", raw(WebSearchInput{Query: "公开价格"}))
	if err != nil {
		t.Fatal(err)
	}

	var found WebResult
	if err := json.Unmarshal(result, &found); err != nil || len(found.Sources) != 1 {
		t.Fatal("unsafe source or invalid results", err)
	}

	for i := 0; i < 5; i++ {
		result, err = remote.Call(ctx, "read_webpage", raw(WebReadInput{URL: found.Sources[0].URL}))
		if err != nil {
			t.Fatal(err)
		}
	}

	var read WebResult
	if err := json.Unmarshal(result, &read); err != nil || len(read.Sources) != 1 || !read.Sources[0].Truncated || len([]rune(read.Sources[0].Content)) != 6000 {
		t.Fatal("read not bounded", err)
	}

	if read.Sources[0].ID != found.Sources[0].ID {
		t.Fatal("source identity changed")
	}

	if _, err := service.Read(ctx, WebReadInput{URL: found.Sources[0].URL}); err == nil {
		t.Fatal("read budget ignored")
	}

	for i := 0; i < 2; i++ {
		if _, err := service.Search(ctx, WebSearchInput{Query: "public"}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := service.Search(ctx, WebSearchInput{Query: "public"}); err == nil {
		t.Fatal("search budget ignored")
	}
}

func TestWebPublicURLsAndFailures(t *testing.T) {
	service := fixtureWeb(t)
	for _, value := range []string{"http://example.com", "https://localhost/x", "https://user:password@example.com", "https://example.com:8443", "https://127.0.0.1/x", "https://example.com/#fragment", "file:///etc/passwd"} {
		if service.publicURL(context.Background(), value) {
			t.Fatal("unsafe URL", value)
		}
	}

	for _, address := range []string{"10.0.0.1", "169.254.169.254", "100.64.0.1", "::1", "fc00::1", "::ffff:192.168.1.1"} {
		service.resolve = func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr(address)}, nil
		}

		if service.publicURL(context.Background(), "https://example.com") {
			t.Fatal("private DNS answer accepted")
		}
	}

	for _, code := range []int{401, 429, 500} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte("provider-secret-detail"))
		}))
		service.endpoint = server.URL
		var out WebResult
		err := service.request(context.Background(), "/search", map[string]string{"query": "public"}, &out)
		server.Close()
		if err == nil || strings.Contains(err.Error(), "provider-secret-detail") {
			t.Fatal("failure leaked or ignored", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.request(ctx, "/search", nil, &WebResult{}); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestConversationCombinesHistoryAndWeb(t *testing.T) {
	hash := sha256.Sum256([]byte("https://example.com/pricing"))
	webID := "web:" + hex.EncodeToString(hash[:8])
	for _, readPage := range []bool{false, true} {
		service := fixtureWeb(t)
		calls := []llm.Call{
			{ID: "history", Name: "read_history", Arguments: `{"message_id":"7"}`},
			{ID: "skill", Name: "load_skill", Arguments: `{"name":"web-research"}`},
			{ID: "search", Name: "search_public_web", Arguments: `{"query":"public pricing","domain":"","time_range":""}`},
		}

		if readPage {
			calls = append(calls, llm.Call{ID: "page", Name: "read_webpage", Arguments: `{"url":"https://example.com/pricing"}`})
		}

		var answer Answer
		_ = json.Unmarshal([]byte(conversationAnswer(false)), &answer)
		answer.SourceIDs = []string{"history:7", webID}
		out, err := run(context.Background(), &rounds{calls: calls, answer: string(raw(answer))}, nil, raw(map[string]any{}), nil, runOptions{conversation: true, history: fakeHistory{}, openWeb: func(ctx context.Context) (Tools, error) { return connectWebService(ctx, service) }})
		if readPage && (err != nil || len(out.Sources) != 2) {
			t.Fatal("combined answer failed", err)
		}

		if !readPage && err == nil {
			t.Fatal("search snippet citation accepted")
		}
	}
}

func TestWebFailureDoesNotBlockHistoryAnswer(t *testing.T) {
	service := fixtureWeb(t)
	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(429) }))
	defer unavailable.Close()
	service.endpoint = unavailable.URL

	var answer Answer
	_ = json.Unmarshal([]byte(conversationAnswer(false)), &answer)
	answer.Reply = "联网服务限流，以下仅依据历史记录。"
	answer.SourceIDs = []string{"history:7"}
	calls := []llm.Call{
		{ID: "history", Name: "read_history", Arguments: `{"message_id":"7"}`},
		{ID: "skill", Name: "load_skill", Arguments: `{"name":"web-research"}`},
		{ID: "search", Name: "search_public_web", Arguments: `{"query":"public","time_range":"","domain":""}`},
	}
	failed := false
	out, err := run(context.Background(), &rounds{calls: calls, answer: string(raw(answer))}, nil, raw(map[string]any{}), func(_ context.Context, event Event) error {
		if event.Name == "search_public_web" && event.Status == "failed" {
			failed = true
		}
		return nil
	}, runOptions{conversation: true, history: fakeHistory{}, openWeb: func(ctx context.Context) (Tools, error) { return connectWebService(ctx, service) }})
	if err != nil || !failed || len(out.Sources) != 1 || out.Sources[0].Kind != "history" {
		t.Fatal("partial answer lost", err)
	}
}

func TestPlainChatDoesNotConnectWeb(t *testing.T) {
	connected := false
	_, err := run(context.Background(), &rounds{answer: conversationAnswer(false)}, nil, raw(map[string]any{}), nil, runOptions{conversation: true, openWeb: func(ctx context.Context) (Tools, error) {
		connected = true
		return connectWebService(ctx, fixtureWeb(t))
	}})
	if err != nil || connected {
		t.Fatal("plain chat opened web", err)
	}
}

type nativeOnceRounder struct {
	model llm.Rounder
	calls int
}

func (m *nativeOnceRounder) Next(ctx context.Context, instructions string, input []json.RawMessage, tools []llm.Tool) (llm.Round, error) {
	m.calls++
	if m.calls == 2 {
		return llm.Round{}, llm.ErrNativeTool
	}
	return m.model.Next(ctx, instructions, input, tools)
}

func TestNativeToolResponseRecoversThroughDeclaredWebTools(t *testing.T) {
	service := fixtureWeb(t)
	hash := sha256.Sum256([]byte("https://example.com/pricing"))
	var answer Answer
	_ = json.Unmarshal([]byte(conversationAnswer(false)), &answer)
	answer.SourceIDs = []string{"web:" + hex.EncodeToString(hash[:8])}
	model := &nativeOnceRounder{model: &rounds{calls: []llm.Call{
		{ID: "load", Name: "load_skill", Arguments: `{"name":"web-research"}`},
		{ID: "search", Name: "search_public_web", Arguments: `{"query":"public pricing","domain":"","time_range":""}`},
		{ID: "read", Name: "read_webpage", Arguments: `{"url":"https://example.com/pricing"}`},
	}, answer: string(raw(answer))}}
	recovered := false
	out, err := run(context.Background(), model, nil, raw(map[string]any{}), func(_ context.Context, event Event) error {
		if event.Kind == "model" && event.Status == "failed" && strings.Contains(string(event.Output), "unsupported_native_tool") {
			recovered = true
		}
		return nil
	}, runOptions{conversation: true, openWeb: func(ctx context.Context) (Tools, error) { return connectWebService(ctx, service) }})
	if err != nil || !recovered || len(out.Sources) != 1 {
		t.Fatal("native tool recovery failed", err)
	}
}

type nativeAlwaysRounder struct{ calls int }

func (m *nativeAlwaysRounder) Next(context.Context, string, []json.RawMessage, []llm.Tool) (llm.Round, error) {
	m.calls++
	return llm.Round{}, llm.ErrNativeTool
}

func TestNativeToolRecoveryIsBounded(t *testing.T) {
	model := &nativeAlwaysRounder{}
	_, err := run(context.Background(), model, nil, raw(map[string]any{}), nil, runOptions{conversation: true})
	if err != llm.ErrNativeTool || model.calls != 4 {
		t.Fatal("unbounded native recovery", model.calls, err)
	}
}
