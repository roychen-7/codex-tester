package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/database"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"github.com/roychen-7/codex-tester/apps/api/internal/querymcp"
	"gorm.io/gorm"
)

type converseFunc func(context.Context, json.RawMessage) (llm.DialogueResponse, error)

func (f converseFunc) Converse(ctx context.Context, in json.RawMessage) (llm.DialogueResponse, error) {
	return f(ctx, in)
}

func TestDialogueLifecycleMySQL(t *testing.T) {
	dsn := config.TestDSN()
	if dsn == "" {
		t.Skip("设置 TEST_MYSQL_DSN 启用连续对话测试")
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("only isolated test database allowed")
	}

	ctx := context.Background()
	db, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	pool, _ := db.DB()
	defer pool.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	rollback := errors.New("rollback fixtures")
	err = db.Transaction(func(tx *gorm.DB) error {
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}

		s := &Store{DB: tx, LLMModel: "test-model", ConversationMode: true}
		user := Account{User: User{Username: fmt.Sprintf("dialogue_%d", time.Now().UnixNano()), DisplayName: "test"}, PasswordHash: "unused", Status: "active"}
		must(tx.Create(&user).Error)
		conv, err := s.CreateConversation(ctx, user.ID, "continuous")
		must(err)
		id := strconv.FormatUint(conv.ID, 10)
		send := func(text string) Message {
			t.Helper()
			requestID, err := messageUUID()
			must(err)
			m, _, err := s.Send(ctx, user.ID, id, requestID, text)
			must(err)
			return m
		}

		state := func() AgentState {
			t.Helper()
			value, err := s.AgentState(ctx, user.ID, id)
			must(err)
			return value
		}

		answer := func(reply string, waiting bool) converseFunc {
			return func(_ context.Context, _ json.RawMessage) (llm.DialogueResponse, error) {
				return llm.DialogueResponse{Result: llm.DialogueResult{Reply: reply, WaitingForUser: waiting, ContextSummary: "用户想优化网站，等待确认目标"}, ProviderID: "test-response"}, nil
			}
		}

		process := func(model converseFunc) {
			t.Helper()
			worked, err := s.ProcessDialogue(ctx, model, time.Second)
			must(err)
			if !worked {
				t.Fatal("no queued turn processed")
			}
		}

		first := send("我想优化网站")
		duplicate, created, err := s.Send(ctx, user.ID, id, first.RequestID, first.Content)
		must(err)
		if created || duplicate.ID != first.ID {
			t.Fatal("idempotency failed")
		}

		initial := state()
		if initial.Task != nil || initial.Run != nil || initial.Turn == nil || initial.Mode != "conversation" {
			t.Fatal("new conversation entered legacy pipeline")
		}

		process(answer("你更关注留存还是收入？", true))
		waiting := state().Turn
		if waiting.Status != RunWaiting || waiting.DeadlineAt != nil || waiting.AssistantMessageID == nil {
			t.Fatal("waiting should be persisted without a request deadline")
		}

		worked, err := s.ProcessDialogue(ctx, answer("unexpected", false), time.Second)
		must(err)
		if worked {
			t.Fatal("waiting turn restarted without user input")
		}

		send("优先留存")
		process(func(_ context.Context, data json.RawMessage) (llm.DialogueResponse, error) {
			var snapshot DialogueContext
			must(json.Unmarshal(data, &snapshot))
			if len(snapshot.Messages) != 3 || snapshot.Summary == "" || snapshot.Messages[0].Content != first.Content || snapshot.Messages[1].Role != "assistant" || snapshot.Messages[2].Content != "优先留存" {
				t.Fatal("conversation context lost", string(data))
			}

			return answer("先改善新用户首次使用体验", false)(ctx, data)
		})
		completed := state().Turn
		if completed.Status != RunSucceeded {
			t.Fatal("direct answer not completed")
		}

		if err := s.completeDialogue(ctx, *completed, llm.DialogueResponse{Result: llm.DialogueResult{Reply: "duplicate", ContextSummary: "x"}}); !errors.Is(err, ErrTaskChanged) {
			t.Fatal("duplicate completion accepted")
		}

		send("再次提问")
		process(func(context.Context, json.RawMessage) (llm.DialogueResponse, error) {
			return llm.DialogueResponse{}, errors.New("secret-provider-detail")
		})
		failed := state().Turn
		if failed.Status != RunFailed || strings.Contains(*failed.ErrorMessage, "secret-provider") {
			t.Fatal("failed state or redaction invalid")
		}

		failedID := strconv.FormatUint(failed.ID, 10)
		retried, err := s.RetryDialogue(ctx, user.ID, id, failedID)
		must(err)
		again, err := s.RetryDialogue(ctx, user.ID, id, failedID)
		must(err)
		if retried.ID != again.ID || retried.AttemptNo != 2 {
			t.Fatal("retry is not idempotent")
		}

		if err := s.CancelDialogue(ctx, user.ID+1, id, strconv.FormatUint(retried.ID, 10)); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-user cancel accepted")
		}

		process(func(_ context.Context, _ json.RawMessage) (llm.DialogueResponse, error) {
			send("改为讨论收入")
			return answer("late answer must not be saved", false)(ctx, nil)
		})
		var cancelled AgentRun
		must(tx.First(&cancelled, retried.ID).Error)
		if cancelled.Status != RunCancelled {
			t.Fatal("old turn not cancelled")
		}

		if _, err := s.RetryDialogue(ctx, user.ID, id, failedID); !errors.Is(err, ErrTaskChanged) {
			t.Fatal("stale retry accepted")
		}

		process(answer("继续讨论收入", false))
		var count int64
		must(tx.Model(&Message{}).Where("conversation_id = ? AND content = ?", conv.ID, "late answer must not be saved").Count(&count).Error)
		if count != 0 {
			t.Fatal("cancelled reply was saved")
		}

		send("停止这个问题")
		pending := state().Turn
		pendingID := strconv.FormatUint(pending.ID, 10)
		must(s.CancelDialogue(ctx, user.ID, id, pendingID))
		must(s.CancelDialogue(ctx, user.ID, id, pendingID))
		if state().Turn.Status != RunCancelled {
			t.Fatal("cancel did not persist")
		}

		send("排队超时")
		expired := state().Turn
		must(tx.Model(&AgentRun{}).Where("id = ?", expired.ID).Update("deadline_at", time.Now().Add(-time.Second)).Error)
		worked, err = s.ProcessDialogue(ctx, answer("must not call model", false), time.Second)
		must(err)
		if worked || state().Turn.Status != RunFailed {
			t.Fatal("expired queue entry was executed")
		}

		send("模型限流提示")
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("secret-provider-body"))
		}))
		defer provider.Close()
		model := llm.NewOpenAI(config.LLM{BaseURL: provider.URL, Model: "test-model", Timeout: time.Second})
		process(model.Converse)
		limited := state().Turn
		if limited.Status != RunFailed || limited.ErrorMessage == nil || *limited.ErrorMessage != "模型服务限流或额度不足，请稍后重试" {
			t.Fatal("classified failure did not reach persisted agent state")
		}

		send("查完再问")
		evidence := []agent.Evidence{{SQL: "SELECT count(*) FROM analytics.users", Parameters: []string{}, Purpose: "模拟客户数", WindowStart: "2026-09-01T00:00:00Z", WindowEnd: "2026-10-01T00:00:00Z", Result: querymcp.Result{ResultID: "saved-result", Columns: []string{"n"}, Types: []string{"INT8"}, Rows: [][]any{{"3"}}, QueriedAt: time.Now().UTC()}}}
		evidenceJSON, err := json.Marshal(evidence)
		must(err)
		process(func(_ context.Context, _ json.RawMessage) (llm.DialogueResponse, error) {
			run := state().Turn
			record := s.dialogueRecorder(*run)
			must(record(ctx, agent.Event{Sequence: 1, Kind: "model", Name: "responses", Status: RunSucceeded}))
			must(record(ctx, agent.Event{Sequence: 2, Kind: "tool", Name: "execute_query", Status: RunRunning, Input: json.RawMessage(`{"query_id":"one-use"}`)}))
			must(record(ctx, agent.Event{Sequence: 2, Kind: "tool", Name: "execute_query", Status: RunSucceeded, Output: evidenceJSON}))
			return llm.DialogueResponse{Result: llm.DialogueResult{Reply: "模拟数据3人，继续按什么口径？", WaitingForUser: true, ContextSummary: "已查询模拟数据", ResultIDs: []string{"saved-result"}}, Evidence: evidenceJSON, StepsRecorded: true}, nil
		})
		savedTurn := state().Turn
		if savedTurn.Status != RunWaiting || savedTurn.AssistantMessageID == nil || state().TurnPhase != "execute_query" {
			t.Fatal("tool progress or waiting result not saved")
		}
		savedMessage := strconv.FormatUint(*savedTurn.AssistantMessageID, 10)
		out, err := s.DialogueEvidence(ctx, user.ID, id, savedMessage)
		must(err)
		if len(out.Evidence) != 1 || out.Evidence[0].Result.ResultID != "saved-result" {
			t.Fatal("message evidence missing")
		}
		page, err := s.Messages(ctx, user.ID, id, "", 100)
		must(err)
		if !page.Items[0].HasEvidence {
			t.Fatal("saved assistant message not annotated")
		}
		if _, err := s.DialogueEvidence(ctx, user.ID+1, id, savedMessage); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-user evidence accepted")
		}
		other, err := s.CreateConversation(ctx, user.ID, "other")
		must(err)
		if _, err := s.DialogueEvidence(ctx, user.ID, strconv.FormatUint(other.ID, 10), savedMessage); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-conversation evidence accepted")
		}
		history, err := dialogueHistory(tx, other.ID)
		must(err)
		if len(history) != 0 {
			t.Fatal("history leaked to another conversation")
		}
		send("沿用刚才的数据")
		process(func(_ context.Context, data json.RawMessage) (llm.DialogueResponse, error) {
			var snapshot DialogueContext
			must(json.Unmarshal(data, &snapshot))
			if len(snapshot.Evidence) != 1 || snapshot.Evidence[0].Result.ResultID != "saved-result" || snapshot.SkillVersion == "" {
				t.Fatal("next turn lost query history")
			}
			return answer("继续分析已保存的数据", false)(ctx, data)
		})
		if err := s.dialogueRecorder(*savedTurn)(ctx, agent.Event{Sequence: 3, Kind: "tool", Name: "execute_query", Status: RunRunning}); !errors.Is(err, ErrTaskChanged) {
			t.Fatal("terminal turn accepted a late tool event")
		}

		// Sources alone must survive persistence and mark the assistant message.
		send("历史来源持久化")
		sourceJSON, err := json.Marshal([]agent.Source{{ID: "history:1", Kind: "history", Content: "原文"}})
		must(err)
		process(func(context.Context, json.RawMessage) (llm.DialogueResponse, error) {
			return llm.DialogueResponse{Result: llm.DialogueResult{Reply: "历史回答", ContextSummary: "历史", SourceIDs: []string{"history:1"}, Conclusions: []llm.Conclusion{{Text: "历史支持的结论", Kind: "fact", SourceIDs: []string{"history:1"}, Rationale: "用户原话明确表达目标"}}}, Sources: sourceJSON}, nil
		})
		sourceMessage := strconv.FormatUint(*state().Turn.AssistantMessageID, 10)
		sourceOutcome, err := s.DialogueEvidence(ctx, user.ID, id, sourceMessage)
		must(err)
		if len(sourceOutcome.Answer.Conclusions) != 1 || sourceOutcome.Answer.Conclusions[0].SourceIDs[0] != "history:1" {
			t.Fatal("conclusion mapping lost")
		}
		if len(sourceOutcome.Sources) != 1 || sourceOutcome.Sources[0].Content != "原文" {
			t.Fatal("sources lost")
		}
		sourcePage, err := s.Messages(ctx, user.ID, id, "", 100)
		must(err)
		if !sourcePage.Items[0].HasEvidence {
			t.Fatal("source-only reply missing annotation")
		}
		if _, err := s.DialogueEvidence(ctx, user.ID+1, id, sourceMessage); !errors.Is(err, ErrNotFound) {
			t.Fatal("source privacy failure")
		}

		// A disabled account cannot receive a late model answer.
		send("账户即将停用")
		process(func(_ context.Context, _ json.RawMessage) (llm.DialogueResponse, error) {
			must(tx.Model(&Account{}).Where("id = ?", user.ID).Update("status", "disabled").Error)
			return answer("must not save", false)(ctx, nil)
		})
		if state().Turn.Status != RunFailed {
			t.Fatal("disabled account reply accepted")
		}

		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}
