package agent

import (
	"encoding/json"
	"testing"

	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"github.com/roychen-7/codex-tester/apps/api/internal/querymcp"
)

func TestConclusionReferencesAreValidatedAndRetained(t *testing.T) {
	for _, tc := range []struct {
		name           string
		source, result string
		fail           bool
	}{
		{"history and query", "history:1", "query-1", false},
		{"web", "web:1", "", false},
		{"invented source", "history:other", "", true},
		{"invented query", "history:1", "query-other", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var answer Answer
			if err := json.Unmarshal([]byte(conversationAnswer(false)), &answer); err != nil {
				t.Fatal(err)
			}

			item := llm.Conclusion{Text: "建议验证新手体验", Kind: "recommendation", SourceIDs: []string{tc.source}, Rationale: "历史目标与公开产品机制支持优先验证", Limitations: "并未证明流失原因"}
			if tc.result != "" {
				item.ResultIDs = []string{tc.result}
			}

			answer.Conclusions = []llm.Conclusion{item}
			out, err := finishConversation(Outcome{Answer: answer, Sources: []Source{{ID: "history:1", Kind: "history"}, {ID: "web:1", Kind: "web"}}}, []Evidence{{Result: querymcp.Result{ResultID: "query-1"}}})
			if (err != nil) != tc.fail {
				t.Fatal("unexpected validation", err)
			}

			if !tc.fail && (len(out.Sources) != 1 || len(out.Answer.SourceIDs) != 1 || (tc.result != "" && len(out.Evidence) != 1)) {
				t.Fatal("per-conclusion evidence lost")
			}
		})
	}
}
