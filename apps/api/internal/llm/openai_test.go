package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/config"
)

const validResult = `{"reply":"请补充时间范围","requirement":{"goal":"分析留存","details":[]},"missing_information":["时间范围？"],"ready_for_confirmation":false,"context_summary":"用户希望分析留存，时间待确认"}`

func TestResponsesAdapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("unexpected endpoint or authentication")
		}

		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["store"] != false || body["model"] != "test-model" {
			t.Error("invalid request configuration")
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id": "resp_test", "status": "completed",
			"output": []any{
				map[string]any{"type": "reasoning"},
				map[string]any{"type": "message", "role": "assistant", "content": []any{
					map[string]any{"type": "output_text", "text": validResult},
				}},
			},
			"usage": map[string]any{"input_tokens": 100, "output_tokens": 50},
		})
	}))
	defer server.Close()

	client := NewOpenAI(config.LLM{APIKey: "test-key", BaseURL: server.URL + "/v1", Model: "test-model", Timeout: time.Second})
	response, err := client.Generate(context.Background(), json.RawMessage(`{"messages":[]}`))
	if err != nil || response.Result.Reply != "请补充时间范围" || response.ProviderID != "resp_test" || response.InputTokens == nil || *response.InputTokens != 100 {
		t.Fatalf("unexpected response: %+v %v", response, err)
	}
}

func TestResponsesFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"auth", 401, "secret-provider-body"},
		{"limit", 429, "secret-provider-body"},
		{"server", 500, "secret-provider-body"},
		{"malformed", 200, "secret-provider-body"},
		{"incomplete", 200, `{"status":"incomplete"}`},
		{"refusal", 200, `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"secret-provider-body"}]}]}`},
		{"oversized", 200, strings.Repeat("x", 1024*1024+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := NewOpenAI(config.LLM{APIKey: "test-key", BaseURL: server.URL, Timeout: time.Second})
			_, err := client.Generate(context.Background(), json.RawMessage(`{}`))
			if err == nil || strings.Contains(err.Error(), "secret-provider-body") || strings.Contains(err.Error(), "test-key") {
				t.Fatal("provider failure accepted or secret leaked", err)
			}
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(150 * time.Millisecond):
		}
	}))
	defer server.Close()
	client := NewOpenAI(config.LLM{BaseURL: server.URL, Timeout: 20 * time.Millisecond})
	if _, err := client.Generate(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("timeout ignored")
	}
}

func TestResultValidation(t *testing.T) {
	for _, data := range []string{
		`{}`, validResult + `{}`, strings.Replace(validResult, `false`, `true`, 1),
		strings.Replace(validResult, `"details":[]`, `"details":null`, 1),
		strings.Replace(validResult, `"reply":`, `"unexpected":0,"reply":`, 1),
		strings.Replace(validResult, `"ready_for_confirmation":false,`, ``, 1),
		strings.Replace(validResult, `"ready_for_confirmation":false`, `"ready_for_confirmation":null`, 1),
	} {
		if _, err := DecodeResult([]byte(data)); err == nil {
			t.Fatal("invalid result accepted")
		}
	}
}
