package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"github.com/roychen-7/codex-tester/apps/api/internal/querymcp"
)

func conversationAnswer(waiting bool, ids ...string) string {
	questions := []string{}
	if waiting {
		questions = append(questions, "按客户还是按订单？")
	}
	if ids == nil {
		ids = []string{}
	}
	return string(raw(Answer{
		Reply: "模拟数据，继续讨论", Questions: questions, ResultIDs: ids,
		WaitingForUser: &waiting, ContextSummary: "用户在讨论充值口径",
	}))
}

func TestConversationSelectsToolsOnlyWhenNeeded(t *testing.T) {
	for _, waiting := range []bool{false, true} {
		connected := false
		out, err := run(context.Background(), &rounds{answer: conversationAnswer(waiting)}, nil, raw(map[string]any{}), nil, runOptions{
			conversation: true,
			open: func(context.Context) (Tools, error) {
				connected = true
				return nil, errors.New("MCP offline")
			},
		})
		if err != nil || connected || *out.Answer.WaitingForUser != waiting || len(out.Evidence) != 0 {
			t.Fatalf("plain chat should not depend on MCP: %v %#v", err, out)
		}
	}

	// A selected but unavailable tool is fed back to the model, which can explain
	// the limitation without inventing citations or failing unrelated chat.
	model := &rounds{calls: skillCalls()[:1], answer: conversationAnswer(false)}
	_, err := run(context.Background(), model, nil, raw(map[string]any{}), nil, runOptions{
		conversation: true, open: func(context.Context) (Tools, error) { return nil, errors.New("MCP offline") },
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConversationQueriesBeforeAskingAndResumesFromEvidence(t *testing.T) {
	calls := append(skillCalls(),
		llm.Call{ID: "prepare", Name: "prepare_query", Arguments: `{"sql":"SELECT count(*) FROM analytics.users","parameters":[],"purpose":"count","window_start":"2026-09-01T00:00:00Z","window_end":"2026-10-01T00:00:00Z"}`},
		llm.Call{ID: "execute", Name: "execute_query", Arguments: `{"query_id":"ticket"}`},
	)
	remote := &fakeTools{}
	out, err := run(context.Background(), &rounds{calls: calls, answer: conversationAnswer(true, "result")}, remote, raw(map[string]any{}), nil, runOptions{conversation: true})
	if err != nil || len(out.Evidence) != 1 || !*out.Answer.WaitingForUser || len(remote.invoked) != 2 {
		t.Fatalf("query then ask failed: %v %#v", err, out)
	}

	prior := out.Evidence
	for _, tc := range []struct {
		name  string
		calls []llm.Call
		cite  string
		fail  bool
	}{
		{"read saved result", []llm.Call{{ID: "read", Name: "read_result", Arguments: `{"result_id":"result"}`}}, "result", false},
		{"unread historical citation", nil, "result", true},
		{"foreign result", []llm.Call{{ID: "read", Name: "read_result", Arguments: `{"result_id":"foreign"}`}}, "foreign", true},
		{"fabricated result", nil, "invented", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &rounds{calls: tc.calls, answer: conversationAnswer(false, tc.cite)}
			out, err := run(context.Background(), model, nil, raw(map[string]any{}), nil, runOptions{conversation: true, prior: prior})
			if (err != nil) != tc.fail {
				t.Fatal("unexpected result", err)
			}
			if !tc.fail && (len(out.Evidence) != 1 || out.Evidence[0].Result.ResultID != "result") {
				t.Fatal("historical provenance lost")
			}
		})
	}
}

type inspectConversation struct{ t *testing.T }

func (m inspectConversation) Next(_ context.Context, _ string, input []json.RawMessage, tools []llm.Tool) (llm.Round, error) {
	var message struct {
		Content string `json:"content"`
	}
	var snapshot map[string]json.RawMessage
	if json.Unmarshal(input[0], &message) != nil || json.Unmarshal([]byte(message.Content), &snapshot) != nil {
		m.t.Fatal("invalid initial context")
	}
	if _, ok := snapshot["evidence"]; ok {
		m.t.Fatal("full rows eagerly exposed instead of result catalogue")
	}
	var catalogue []map[string]any
	if json.Unmarshal(snapshot["previous_results"], &catalogue) != nil || len(catalogue) != 1 || len(tools) != 1 || tools[0].Name != "read_result" {
		m.t.Fatal("saved result tool unavailable without MCP")
	}
	return llm.Round{Text: conversationAnswer(false)}, nil
}

func TestConversationContextAndValidation(t *testing.T) {
	c := Conversation{Model: inspectConversation{t: t}}
	_, err := c.Converse(context.Background(), raw(map[string]any{
		"messages": []any{}, "evidence": []Evidence{{Result: querymcp.Result{ResultID: "old"}}},
	}))
	if err != nil {
		t.Fatal(err)
	}

	for _, answer := range []string{
		`{"reply":"hello","questions":[],"result_ids":[],"context_summary":"x"}`,
		`{"reply":"hello","questions":[],"result_ids":[],"waiting_for_user":true,"context_summary":"x"}`,
		`{"reply":"hello","questions":null,"result_ids":[],"waiting_for_user":false,"context_summary":"x"}`,
		`{"reply":"hello","questions":[],"result_ids":["made-up"],"waiting_for_user":false,"context_summary":"x"}`,
	} {
		_, err := run(context.Background(), &rounds{answer: answer}, nil, raw(map[string]any{}), nil, runOptions{conversation: true})
		if err == nil {
			t.Fatal("accepted invalid conversational result", answer)
		}
	}
}
