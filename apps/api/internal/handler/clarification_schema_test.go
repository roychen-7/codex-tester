package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/roychen-7/codex-tester/apps/api/internal/chat"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/database"
	"gorm.io/gorm"
)

func TestClarificationSchemaMySQL(t *testing.T) {
	dsn := config.TestDSN()
	if dsn == "" {
		t.Skip("设置 TEST_MYSQL_DSN 启用真实 MySQL 约束测试")
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("仅允许使用 _test 结尾的独立数据库")
	}

	ctx := context.Background()
	db, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	pool, _ := db.DB()
	defer pool.Close()
	for range 2 {
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}

	if err := database.Ready(ctx, db); err != nil {
		t.Fatal(err)
	}

	rollback := errors.New("rollback schema test fixtures")
	err = db.Transaction(func(tx *gorm.DB) error {
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		reject := func(err error, number uint16) {
			t.Helper()
			var sqlErr *mysql.MySQLError
			if !errors.As(err, &sqlErr) || sqlErr.Number != number {
				t.Fatalf("expected MySQL constraint error %d, got %v", number, err)
			}
		}

		user := chat.Account{
			User:         chat.User{Username: fmt.Sprintf("schema_%d", time.Now().UnixNano()), DisplayName: "Schema test"},
			PasswordHash: "not-a-login-account",
			Status:       "active",
		}
		must(tx.Create(&user).Error)

		first := chat.Conversation{UserID: user.ID, Title: "first"}
		second := chat.Conversation{UserID: user.ID, Title: "second"}
		must(tx.Create(&first).Error)
		must(tx.Create(&second).Error)

		message := chat.Message{ConversationID: first.ID, RequestID: "11111111-1111-4111-8111-111111111111", Role: "user", Content: "请分析"}
		other := chat.Message{ConversationID: second.ID, RequestID: "22222222-2222-4222-8222-222222222222", Role: "user", Content: "其他会话"}
		answer := chat.Message{ConversationID: first.ID, RequestID: "33333333-3333-4333-8333-333333333333", Role: "assistant", Content: "请补充时间范围"}
		must(tx.Create(&message).Error)
		must(tx.Create(&other).Error)
		must(tx.Create(&answer).Error)

		newTask := func() chat.AgentTask {
			return chat.AgentTask{
				ConversationID:     first.ID,
				OriginMessageID:    message.ID,
				Status:             chat.TaskClarifying,
				Requirement:        json.RawMessage(`{"goal":"分析使用情况"}`),
				MissingInformation: json.RawMessage(`["时间范围"]`),
				Revision:           1,
			}
		}

		task := newTask()
		must(tx.Create(&task).Error)
		var restored chat.AgentTask
		must(tx.First(&restored, task.ID).Error)
		if !json.Valid(restored.Requirement) || !strings.Contains(string(restored.Requirement), "分析使用情况") {
			t.Fatal("GORM JSON round trip failed")
		}

		invalid := newTask()
		invalid.OriginMessageID = other.ID
		invalid.Status = chat.TaskCancelled
		reject(tx.Create(&invalid).Error, 1452)

		anotherMessage := message
		anotherMessage.ID = 0
		anotherMessage.RequestID = "44444444-4444-4444-8444-444444444444"
		must(tx.Create(&anotherMessage).Error)
		invalid = newTask()
		invalid.OriginMessageID = anotherMessage.ID
		reject(tx.Create(&invalid).Error, 1062)

		reject(tx.Model(&task).Update("revision", 0).Error, 3819)
		reject(tx.Model(&task).Update("status", "invalid").Error, 3819)
		reject(tx.Model(&task).Update("status", chat.TaskConfirmed).Error, 3819)
		reject(tx.Model(&task).Update("requirement", "[]").Error, 3819)
		reject(tx.Model(&task).Update("missing_information", "{}").Error, 3819)

		now := time.Now().UTC().Truncate(time.Microsecond)
		must(tx.Model(&task).Updates(map[string]any{"status": chat.TaskConfirmed, "confirmed_at": now}).Error)
		must(tx.Create(&invalid).Error)
		reject(tx.Model(&task).Updates(map[string]any{"status": chat.TaskClarifying, "confirmed_at": nil}).Error, 1062)

		newRun := func(attempt uint32) chat.LLMRun {
			return chat.LLMRun{
				ConversationID:   first.ID,
				TaskID:           &task.ID,
				TriggerMessageID: message.ID,
				AttemptNo:        attempt,
				Status:           chat.RunPending,
				Model:            "test-model",
				PromptVersion:    "clarification-v1",
				ContextSnapshot:  json.RawMessage(`{"task_revision":1,"message_ids":[]}`),
			}
		}

		run := newRun(1)
		must(tx.Create(&run).Error)
		var restoredRun chat.LLMRun
		must(tx.First(&restoredRun, run.ID).Error)
		if restoredRun.InputTokens != nil || restoredRun.OutputTokens != nil || restoredRun.Result != nil {
			t.Fatal("unknown usage/result must remain NULL")
		}

		concurrent := newRun(2)
		reject(tx.Create(&concurrent).Error, 1062)
		reject(tx.Model(&run).Update("status", chat.RunRunning).Error, 3819)
		must(tx.Model(&run).Updates(map[string]any{
			"status": chat.RunRunning, "started_at": now, "deadline_at": now.Add(time.Minute),
		}).Error)
		must(tx.Model(&run).Updates(map[string]any{"status": chat.RunFailed, "finished_at": now}).Error)

		duplicateAttempt := newRun(1)
		reject(tx.Create(&duplicateAttempt).Error, 1062)
		must(tx.Create(&concurrent).Error)
		reject(tx.Model(&concurrent).Updates(map[string]any{
			"status": chat.RunSucceeded, "started_at": now, "finished_at": now,
		}).Error, 3819)

		must(tx.Model(&concurrent).Updates(map[string]any{
			"status": chat.RunSucceeded, "started_at": now, "finished_at": now,
			"assistant_message_id": answer.ID, "result": `{"reply":"请补充时间范围"}`,
		}).Error)

		secondAnswer := answer
		secondAnswer.ID = 0
		secondAnswer.RequestID = "55555555-5555-4555-8555-555555555555"
		must(tx.Create(&secondAnswer).Error)
		reject(tx.Model(&run).Updates(map[string]any{
			"status": chat.RunSucceeded, "assistant_message_id": secondAnswer.ID,
			"result": `{"reply":"重复回复"}`,
		}).Error, 1062)

		foreignRun := newRun(3)
		foreignRun.ConversationID = second.ID
		foreignRun.TriggerMessageID = other.ID
		reject(tx.Create(&foreignRun).Error, 1452)
		foreignRun.TaskID = nil
		must(tx.Create(&foreignRun).Error)
		reject(tx.Model(&foreignRun).Updates(map[string]any{
			"status": chat.RunSucceeded, "started_at": now, "finished_at": now,
			"assistant_message_id": secondAnswer.ID, "result": `{"reply":"跨会话"}`,
		}).Error, 1452)

		reject(tx.Model(&first).Update("context_summary", "只有摘要").Error, 3819)
		reject(tx.Model(&first).Updates(map[string]any{
			"context_summary": "错误来源", "summary_through_message_id": other.ID,
		}).Error, 1452)
		must(tx.Model(&first).Updates(map[string]any{
			"context_summary": "分析使用情况，时间待确认", "summary_through_message_id": answer.ID,
		}).Error)
		must(tx.First(&first, first.ID).Error)
		if first.ContextSummary == nil || first.SummaryThroughMessageID == nil || *first.SummaryThroughMessageID != answer.ID {
			t.Fatal("summary fields did not persist")
		}

		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}
