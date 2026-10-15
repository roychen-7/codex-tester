package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/config"
)

func TestToolRoundPreservesStatelessReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Store    bool              `json:"store"`
			Parallel bool              `json:"parallel_tool_calls"`
			Include  []string          `json:"include"`
			Input    []json.RawMessage `json:"input"`
		}

		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Store || body.Parallel || len(body.Include) != 1 || len(body.Input) != 1 {
			t.Error("invalid stateless tool request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp","status":"completed","output":[{"type":"reasoning","id":"rs","encrypted_content":"opaque","summary":[]},{"type":"function_call","id":"fc","call_id":"call","name":"load_skill","arguments":"{\"name\":\"ddshub-analysis\"}"}]}`))
	}))
	defer server.Close()
	client := NewOpenAI(config.LLM{APIKey: "test-only", BaseURL: server.URL, Model: "test", Timeout: time.Second})
	out, err := client.Next(context.Background(), "test", []json.RawMessage{json.RawMessage(`{"role":"user","content":"test"}`)}, nil)
	if err != nil || len(out.Items) != 2 || len(out.Calls) != 1 || out.Calls[0].ID != "call" {
		t.Fatalf("tool round invalid: %#v %v", out, err)
	}

	var reasoning map[string]any
	if json.Unmarshal(out.Items[0], &reasoning) != nil || reasoning["encrypted_content"] != "opaque" {
		t.Fatal("reasoning lost across rounds")
	}
}

func TestRoundRejectsUnconfiguredNativeSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","status":"completed","output":[{"type":"web_search_call","id":"native","status":"completed"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"untracked sources"}]}]}`))
	}))
	defer server.Close()

	client := NewOpenAI(config.LLM{APIKey: "test-only", BaseURL: server.URL, Model: "test", Timeout: time.Second})
	if _, err := client.Next(context.Background(), "test", []json.RawMessage{json.RawMessage(`{"role":"user","content":"test"}`)}, nil); err == nil {
		t.Fatal("native tool result accepted without provenance")
	}
}

func TestRecoveryToolChoiceIsSentToProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ToolChoice struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"tool_choice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.ToolChoice.Type != "function" || request.ToolChoice.Name != "search_public_web" {
			t.Error("missing explicit recovery tool choice")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","status":"completed","output":[{"type":"function_call","call_id":"s","name":"search_public_web","arguments":"{}"}]}`))
	}))
	defer server.Close()
	client := NewOpenAI(config.LLM{APIKey: "test-only", BaseURL: server.URL, Model: "test", Timeout: time.Second})
	out, err := client.Next(WithToolChoice(context.Background(), "search_public_web"), "test", []json.RawMessage{json.RawMessage(`{"role":"user","content":"test"}`)}, nil)
	if err != nil || len(out.Calls) != 1 {
		t.Fatal("recovery choice failed", err)
	}
	code, message := FailureDetails(ErrNativeTool)
	if code != "unsupported_native_tool" || message == "暂时无法生成回复，请重试" {
		t.Fatal("native failure not classified")
	}
}
