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
	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/database"
	"github.com/roychen-7/codex-tester/apps/api/internal/querymcp"
	"gorm.io/gorm"
)

func TestAnalysisLifecycleMySQL(t *testing.T) {
	dsn := config.TestDSN()
	if dsn == "" {
		t.Skip("设置 TEST_MYSQL_DSN 启用分析持久化测试")
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

	rollback := errors.New("rollback")
	err = db.Transaction(func(tx *gorm.DB) error {
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}

		s := &Store{DB: tx, LLMModel: "test-model", AnalysisEnabled: true}
		user := Account{User: User{Username: fmt.Sprintf("analysis_%d", time.Now().UnixNano()), DisplayName: "test"}, PasswordHash: "unused", Status: "active"}
		must(tx.Create(&user).Error)
		conv, err := s.CreateConversation(ctx, user.ID, "test")
		must(err)
		id := strconv.FormatUint(conv.ID, 10)
		message := Message{ConversationID: conv.ID, RequestID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Role: "user", Content: "count users"}
		must(tx.Create(&message).Error)
		task := AgentTask{ConversationID: conv.ID, OriginMessageID: message.ID, Status: TaskAwaitingConfirmation, Revision: 1, Requirement: json.RawMessage(`{"goal":"count","details":[]}`), MissingInformation: json.RawMessage(`[]`)}
		must(tx.Create(&task).Error)
		taskID := strconv.FormatUint(task.ID, 10)
		_, err = s.Confirm(ctx, user.ID, id, taskID, 1)
		must(err)
		run, err := s.StartAnalysis(ctx, user.ID, id, taskID, 1, false)
		must(err)
		again, err := s.StartAnalysis(ctx, user.ID, id, taskID, 1, false)
		must(err)
		if run.ID == 0 || again.ID != run.ID {
			t.Fatal("duplicate confirmation queued twice")
		}

		runID := strconv.FormatUint(run.ID, 10)
		if err := s.CancelAnalysis(ctx, user.ID+1, id, runID); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross owner cancel", err)
		}
		must(s.CancelAnalysis(ctx, user.ID, id, runID))
		must(tx.Model(&AnalysisRun{}).Where("id = ?", run.ID).Update("phase", "cancelled").Error)
		out := agent.Outcome{Answer: agent.Answer{Reply: "模拟数据3人", Questions: []string{}, ResultIDs: []string{"r1"}}, Evidence: []agent.Evidence{{SQL: "SELECT count(*) FROM analytics.users", Parameters: []string{}, Result: querymcp.Result{ResultID: "r1", Columns: []string{"count"}, Types: []string{"INT8"}, Rows: [][]any{{"3"}}}}}}
		if err := s.completeAnalysis(ctx, run, out); !errors.Is(err, ErrTaskChanged) {
			t.Fatal("cancelled run wrote answer", err)
		}
		run, err = s.StartAnalysis(ctx, user.ID, id, taskID, 1, true)
		must(err)
		if run.AttemptNo != 2 {
			t.Fatal("retry must have new attempt")
		}
		must(tx.Model(&run).Updates(map[string]any{"status": RunRunning, "started_at": time.Now().UTC()}).Error)
		must(s.recordAnalysis(ctx, run, agent.Event{Sequence: 1, Kind: "model", Name: "responses", Status: RunRunning, Input: json.RawMessage(`{}`)}))
		must(s.completeAnalysis(ctx, run, out))
		if err := s.completeAnalysis(ctx, run, out); !errors.Is(err, ErrTaskChanged) {
			t.Fatal("duplicate completion accepted", err)
		}

		result, err := s.AnalysisResult(ctx, user.ID, id, strconv.FormatUint(run.ID, 10))
		must(err)
		if len(result.Evidence) != 1 || result.Answer.Reply != out.Answer.Reply {
			t.Fatal("lost evidence")
		}

		if _, err := s.AnalysisResult(ctx, user.ID+1, id, strconv.FormatUint(run.ID, 10)); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross owner result", err)
		}
		must(tx.Model(&AgentTask{}).Where("id = ?", task.ID).Update("revision", 2).Error)
		stale, err := s.StartAnalysis(ctx, user.ID, id, taskID, 2, false)
		must(err)
		must(tx.Model(&stale).Updates(map[string]any{"status": RunRunning, "started_at": time.Now().UTC()}).Error)
		_, _, err = s.Send(ctx, user.ID, id, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "改为统计充值")
		must(err)
		if err := s.completeAnalysis(ctx, stale, out); !errors.Is(err, ErrTaskChanged) {
			t.Fatal("amended request accepted old answer", err)
		}

		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}
