package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"github.com/roychen-7/codex-tester/apps/api/internal/querymcp"
)

type rounds struct {
	calls  []llm.Call
	answer string
	index  int
}

func (m *rounds) Next(_ context.Context, _ string, _ []json.RawMessage, _ []llm.Tool) (llm.Round, error) {
	if m.index == len(m.calls) {
		return llm.Round{Text: m.answer}, nil
	}

	call := m.calls[m.index]
	m.index++
	return llm.Round{Calls: []llm.Call{call}}, nil
}

type fakeTools struct{ invoked []string }

func (f *fakeTools) Definitions() []llm.Tool { return nil }
func (f *fakeTools) Close() error            { return nil }
func (f *fakeTools) Call(_ context.Context, name string, _ json.RawMessage) (json.RawMessage, error) {
	f.invoked = append(f.invoked, name)
	switch name {
	case "prepare_query":
		return raw(querymcp.Prepared{QueryID: "ticket", SQL: "SELECT count(*) FROM analytics.users"}), nil
	case "execute_query":
		return raw(querymcp.Result{ResultID: "result", Columns: []string{"n"}, Types: []string{"INT8"}, Rows: [][]any{{"3"}}}), nil
	default:
		return raw(map[string]string{"dataset": "synthetic"}), nil
	}
}

func skillCalls() []llm.Call {
	return []llm.Call{
		{ID: "load", Name: "load_skill", Arguments: `{"name":"ddshub-analysis"}`},
		{ID: "read", Name: "read_skill_reference", Arguments: `{"name":"ddshub-analysis","path":"references/data-dictionary.md"}`},
	}
}

func TestRunEvidenceAndBoundaries(t *testing.T) {
	calls := append(skillCalls(), llm.Call{ID: "prepare", Name: "prepare_query", Arguments: `{"sql":"SELECT count(*) FROM analytics.users","parameters":[],"purpose":"count","window_start":"2026-09-01T00:00:00Z","window_end":"2026-10-01T00:00:00Z"}`}, llm.Call{ID: "execute", Name: "execute_query", Arguments: `{"query_id":"ticket"}`})
	model := &rounds{calls: calls, answer: `{"reply":"模拟数据共3人","questions":[],"result_ids":["result"]}`}
	remote := &fakeTools{}
	events := 0
	out, err := Run(context.Background(), model, remote, raw(map[string]string{"goal": "count"}), func(_ context.Context, e Event) error { events++; return nil })
	if err != nil || len(out.Evidence) != 1 || len(remote.invoked) != 2 || events != 18 {
		t.Fatalf("unexpected execution: %v, %d events, %#v", err, events, out)
	}

	if out.Evidence[0].SQL != "SELECT count(*) FROM analytics.users" {
		t.Fatal("SQL provenance lost")
	}

	for _, tc := range []struct {
		name      string
		calls     []llm.Call
		answer    string
		wantError bool
	}{
		{"skill gate", []llm.Call{{ID: "x", Name: "execute_query", Arguments: `{"query_id":"ticket"}`}}, `{"reply":"请补充","questions":["时间？"],"result_ids":[]}`, false},
		{"foreign ticket", append(skillCalls(), llm.Call{ID: "x", Name: "execute_query", Arguments: `{"query_id":"other"}`}), `{"reply":"请补充","questions":["时间？"],"result_ids":[]}`, false},
		{"fabricated citation", nil, `{"reply":"3人","questions":[],"result_ids":["invented"]}`, true},
		{"no evidence", nil, `{"reply":"3人","questions":[],"result_ids":[]}`, true},
		{"duplicate call", []llm.Call{{ID: "x", Name: "load_skill", Arguments: `{"name":"ddshub-analysis"}`}, {ID: "x", Name: "load_skill", Arguments: `{"name":"ddshub-analysis"}`}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := &fakeTools{}
			_, err := Run(context.Background(), &rounds{calls: tc.calls, answer: tc.answer}, remote, raw(map[string]string{}), nil)
			if (err != nil) != tc.wantError || len(remote.invoked) != 0 {
				t.Fatalf("unexpected error or remote access: %v, %v", err, remote.invoked)
			}
		})
	}
}

func TestRunStopsBeforeExternalCall(t *testing.T) {
	model := &rounds{calls: skillCalls()}
	_, err := Run(context.Background(), model, &fakeTools{}, raw(map[string]string{}), func(context.Context, Event) error { return errors.New("cancelled") })
	if err == nil || model.index != 0 {
		t.Fatal("cancelled lease still called model")
	}

	if _, err := ReadSkill(SkillName, "../../secret"); err == nil {
		t.Fatal("reference path traversal")
	}
}
