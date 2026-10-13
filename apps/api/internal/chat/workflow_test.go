package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/database"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"gorm.io/gorm"
)

type generateFunc func(context.Context, json.RawMessage) (llm.Response, error)

func (f generateFunc) Generate(ctx context.Context, input json.RawMessage) (llm.Response, error) {
	return f(ctx, input)
}

func TestAgentWorkflowMySQL(t *testing.T) {
	dsn := config.TestDSN()
	if dsn == "" {
		t.Skip("设置 TEST_MYSQL_DSN 启用 Agent 流程测试")
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("仅允许使用独立测试数据库")
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

		s := &Store{DB: tx, LLMModel: "test-model"}
		user := Account{
			User:         User{Username: fmt.Sprintf("workflow_%d", time.Now().UnixNano()), DisplayName: "test"},
			PasswordHash: "not-for-login", Status: "active",
		}
		must(tx.Create(&user).Error)
		conversation, err := s.CreateConversation(ctx, user.ID, "测试需求")
		must(err)
		id := strconv.FormatUint(conversation.ID, 10)

		message, created, err := s.Send(ctx, user.ID, id, "11111111-1111-4111-8111-111111111111", "分析客户留存")
		must(err)
		if !created {
			t.Fatal("initial message not created")
		}

		retryMessage, created, err := s.Send(ctx, user.ID, id, message.RequestID, message.Content)
		must(err)
		if created || retryMessage.ID != message.ID {
			t.Fatal("message retry created another run/message")
		}

		if _, _, err := s.Send(ctx, user.ID, id, "22222222-2222-4222-8222-222222222222", "额外请求"); !errors.Is(err, ErrBusy) {
			t.Fatal("overlapping message accepted", err)
		}

		if _, err := s.AgentState(ctx, user.ID+1, id); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-user state leaked")
		}

		state, err := s.AgentState(ctx, user.ID, id)
		must(err)
		if state.Run == nil || state.Task == nil || state.Run.Status != RunPending {
			t.Fatal("run/task not persisted")
		}

		if _, err := s.Confirm(ctx, user.ID, id, strconv.FormatUint(state.Task.ID, 10), state.Task.Revision); !errors.Is(err, ErrTaskChanged) {
			t.Fatal("confirmed while generating")
		}

		answer := func(ready bool) generateFunc {
			return func(_ context.Context, input json.RawMessage) (llm.Response, error) {
				var snapshot GenerationContext
				must(json.Unmarshal(input, &snapshot))
				last := snapshot.Messages[len(snapshot.Messages)-1]
				questions := []string{"需要分析哪个时间范围？"}
				if ready {
					questions = []string{}
					if snapshot.Summary == "" || len(snapshot.Messages) < 3 {
						t.Fatal("context lost across turns")
					}
				}

				return llm.Response{ProviderID: "resp_test", Result: llm.Result{
					Reply: "正在整理需求，请确认时间范围。",
					Requirement: llm.Requirement{Goal: "客户留存分析", Details: []llm.Detail{
						{Name: "需求", Value: last.Content, Source: "user", SourceMessageIDs: []string{strconv.FormatUint(last.ID, 10)}},
					}},
					MissingInformation: questions, ReadyForConfirmation: ready,
					ContextSummary: "用户希望分析客户留存，等待确认需求。",
				}}, nil
			}
		}

		worked, err := s.ProcessOne(ctx, answer(false), time.Second)
		must(err)
		if !worked {
			t.Fatal("pending run not processed")
		}

		state, err = s.AgentState(ctx, user.ID, id)
		must(err)
		if state.Task.Status != TaskClarifying || state.Run.Status != RunSucceeded || state.Run.AssistantMessageID == nil {
			t.Fatal("clarification not persisted")
		}

		_, _, err = s.Send(ctx, user.ID, id, "22222222-2222-4222-8222-222222222222", "2026年9月，按北京时间，使用过服务的付费用户")
		must(err)
		_, err = s.ProcessOne(ctx, answer(true), time.Second)
		must(err)
		state, err = s.AgentState(ctx, user.ID, id)
		must(err)
		taskID := strconv.FormatUint(state.Task.ID, 10)
		if state.Task.Status != TaskAwaitingConfirmation {
			t.Fatal("ready task was automatically confirmed")
		}

		if _, err := s.Confirm(ctx, user.ID, id, taskID, state.Task.Revision-1); !errors.Is(err, ErrTaskChanged) {
			t.Fatal("stale revision confirmed")
		}

		if _, err := s.Confirm(ctx, user.ID+1, id, taskID, state.Task.Revision); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-user confirmation accepted")
		}

		confirmed, err := s.Confirm(ctx, user.ID, id, taskID, state.Task.Revision)
		must(err)
		_, err = s.Confirm(ctx, user.ID, id, taskID, state.Task.Revision)
		must(err)
		if confirmed.Status != TaskConfirmed {
			t.Fatal("explicit confirmation not persisted")
		}

		_, _, err = s.Send(ctx, user.ID, id, "33333333-3333-4333-8333-333333333333", "改成8月")
		must(err)
		_, err = s.ProcessOne(ctx, generateFunc(func(context.Context, json.RawMessage) (llm.Response, error) {
			return llm.Response{}, errors.New("测试超时")
		}), time.Second)
		must(err)
		state, err = s.AgentState(ctx, user.ID, id)
		must(err)
		if state.Task.Status != TaskClarifying || state.Task.ConfirmedAt != nil || state.Run.Status != RunFailed {
			t.Fatal("amendment/failure state incorrect")
		}

		failedID := strconv.FormatUint(state.Run.ID, 10)
		if _, err := s.Retry(ctx, user.ID+1, id, failedID); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross-user retry accepted")
		}

		retried, err := s.Retry(ctx, user.ID, id, failedID)
		must(err)
		repeated, err := s.Retry(ctx, user.ID, id, failedID)
		must(err)
		if retried.ID != repeated.ID || retried.AttemptNo != 2 {
			t.Fatal("retry was not idempotent")
		}

		_, err = s.ProcessOne(ctx, answer(true), time.Second)
		must(err)
		state, err = s.AgentState(ctx, user.ID, id)
		must(err)
		if state.Run.Status != RunSucceeded {
			t.Fatal("retry did not succeed")
		}

		// A provider callback arriving after its lease expired must never create a reply.
		_, _, err = s.Send(ctx, user.ID, id, "44444444-4444-4444-8444-444444444444", "改成7月")
		must(err)
		_, err = s.ProcessOne(ctx, generateFunc(func(ctx context.Context, input json.RawMessage) (llm.Response, error) {
			must(tx.Model(&LLMRun{}).Where("conversation_id = ? AND status = ?", conversation.ID, RunRunning).
				Updates(map[string]any{"status": RunFailed, "finished_at": time.Now().UTC()}).Error)
			return answer(true)(ctx, input)
		}), time.Second)
		must(err)
		state, err = s.AgentState(ctx, user.ID, id)
		must(err)
		if state.Run.AssistantMessageID != nil || state.Run.Status != RunFailed {
			t.Fatal("late result overwrote failed attempt")
		}

		// Fabricated source IDs must reject the entire result without an orphan reply.
		_, _, err = s.Send(ctx, user.ID, id, "55555555-5555-4555-8555-555555555555", "改成6月")
		must(err)
		var before int64
		must(tx.Model(&Message{}).Where("conversation_id = ?", conversation.ID).Count(&before).Error)
		_, err = s.ProcessOne(ctx, generateFunc(func(ctx context.Context, input json.RawMessage) (llm.Response, error) {
			response, err := answer(true)(ctx, input)
			response.Result.Requirement.Details[0].SourceMessageIDs = []string{"999999999999"}
			return response, err
		}), time.Second)
		must(err)
		state, err = s.AgentState(ctx, user.ID, id)
		must(err)
		var after int64
		must(tx.Model(&Message{}).Where("conversation_id = ?", conversation.ID).Count(&after).Error)
		if state.Run.Status != RunFailed || state.Run.AssistantMessageID != nil || before != after {
			t.Fatal("untrusted source produced a partial result")
		}

		// An expired queue entry is recoverable and must not call the provider.
		_, _, err = s.Send(ctx, user.ID, id, "66666666-6666-4666-8666-666666666666", "改成5月")
		must(err)
		must(tx.Model(&LLMRun{}).Where("conversation_id = ? AND status = ?", conversation.ID, RunPending).
			Update("deadline_at", time.Now().UTC().Add(-time.Second)).Error)
		_, err = s.ProcessOne(ctx, generateFunc(func(context.Context, json.RawMessage) (llm.Response, error) {
			t.Fatal("expired queue entry called provider")
			return llm.Response{}, nil
		}), time.Second)
		must(err)
		state, err = s.AgentState(ctx, user.ID, id)
		must(err)
		if state.Run.Status != RunFailed || state.Run.ErrorCode == nil || *state.Run.ErrorCode != "INTERRUPTED" {
			t.Fatal("expired queue entry did not become retryable")
		}

		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}
