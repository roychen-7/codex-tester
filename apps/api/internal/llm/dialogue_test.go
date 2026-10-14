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

func TestDialogueAdapter(t *testing.T) {
	for _, tc := range []struct {
		name, text     string
		valid, waiting bool
	}{
		{"direct", `{"reply":"留存关注持续使用","waiting_for_user":false,"context_summary":"用户在了解留存"}`, true, false},
		{"ask", `{"reply":"你更关注留存还是收入？","waiting_for_user":true,"context_summary":"用户想优化网站，目标待补充"}`, true, true},
		{"missing state", `{"reply":"hi","context_summary":"hi"}`, false, false},
		{"empty reply", `{"reply":" ","waiting_for_user":false,"context_summary":"hi"}`, false, false},
		{"trailing JSON", `{"reply":"hi","waiting_for_user":false,"context_summary":"hi"}{}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["store"] != false || payload["instructions"] != dialogueInstructions {
					t.Error("invalid conversational request")
				}

				json.NewEncoder(w).Encode(map[string]any{"id": "response", "status": "completed", "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": tc.text}}}}})
			}))
			defer server.Close()

			model := NewOpenAI(config.LLM{BaseURL: server.URL, APIKey: "test", Model: "test", Timeout: time.Second})
			result, err := model.Converse(context.Background(), json.RawMessage(`{"messages":[]}`))
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected result %v", err)
			}

			if tc.valid && result.Result.WaitingForUser != tc.waiting {
				t.Fatal("lost waiting decision")
			}
		})
	}
}
