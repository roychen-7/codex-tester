package agent

import (
	"context"
	"testing"

	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"github.com/roychen-7/codex-tester/apps/api/internal/querymcp"
)

func TestPriorEvidenceSourceIsolation(t *testing.T) {
	prior := []Evidence{{Result: querymcp.Result{ResultID: "old"}}, {Result: querymcp.Result{ResultID: "test", DataSource: "synthetic"}}, {Result: querymcp.Result{ResultID: "real", DataSource: "live"}}}
	live := matchingEvidence(prior, "live")
	if len(live) != 1 || live[0].Result.ResultID != "real" {
		t.Fatal("synthetic evidence crossed into live context")
	}
	if len(matchingEvidence(prior, "")) != 2 {
		t.Fatal("old synthetic records lost")
	}
}

func TestLiveRejectsSyntheticPreparation(t *testing.T) {
	calls := append(skillCalls(), llm.Call{ID: "prepare", Name: "prepare_query", Arguments: `{"sql":"SELECT count(*) FROM analytics.users","parameters":[],"purpose":"count","window_start":"2026-09-01T00:00:00Z","window_end":"2026-10-01T00:00:00Z"}`})
	remote := &fakeTools{}
	_, err := run(context.Background(), &rounds{calls: calls}, remote, raw(map[string]string{"goal": "test"}), nil, runOptions{source: "live"})
	if err == nil {
		t.Fatal("live accepted an unlabelled synthetic ticket")
	}
	for _, name := range remote.invoked {
		if name == "execute_query" {
			t.Fatal("mismatched ticket executed")
		}
	}
}
