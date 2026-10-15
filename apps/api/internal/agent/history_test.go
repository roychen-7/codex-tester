package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
)

type fakeHistory struct{}

func (fakeHistory) Search(context.Context, HistorySearch) (HistoryMatches, error) {
	return HistoryMatches{Items: []Source{{ID: "history:7", Content: "snippet"}}}, nil
}

func (fakeHistory) Read(context.Context, string) ([]Source, error) {
	return []Source{{ID: "history:7", Kind: "history", Content: "original"}}, nil
}

func TestHistoryRequiresReadBeforeCitation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		calls     []llm.Call
		wantError bool
	}{
		{"fabricated", nil, true},
		{"search only", []llm.Call{{ID: "search", Name: "search_history", Arguments: `{"query":"留存","before_id":""}`}}, true},
		{"read", []llm.Call{{ID: "read", Name: "read_history", Arguments: `{"message_id":"7"}`}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var answer Answer
			if err := json.Unmarshal([]byte(conversationAnswer(false)), &answer); err != nil {
				t.Fatal(err)
			}
			answer.SourceIDs = []string{"history:7"}
			model := &rounds{calls: tc.calls, answer: string(raw(answer))}
			out, err := run(context.Background(), model, nil, raw(map[string]any{}), nil, runOptions{conversation: true, history: fakeHistory{}})
			if (err != nil) != tc.wantError {
				t.Fatal(err)
			}

			if err == nil && (len(out.Sources) != 1 || out.Sources[0].Content != "original") {
				t.Fatal("source lost")
			}
		})
	}
}

func TestHistoryToolRejectsOwnerArgument(t *testing.T) {
	_, _, err := callHistory(context.Background(), fakeHistory{}, "search_history", `{"query":"x","before_id":"","user_id":"8"}`)
	if err == nil {
		t.Fatal("model owner accepted")
	}

	_, _, err = callHistory(context.Background(), nil, "read_history", `{"message_id":"7"}`)
	if err == nil {
		t.Fatal("unconfigured history accepted")
	}
}
