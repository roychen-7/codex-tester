package chat

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) WorkDialogue(ctx context.Context, model llm.Converser, timeout time.Duration) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for ctx.Err() == nil {
		worked, err := s.ProcessDialogue(ctx, model, timeout)
		if err != nil && ctx.Err() == nil {
			slog.Error("conversation worker storage operation failed")
		}

		if worked && err == nil {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Store) ProcessDialogue(ctx context.Context, model llm.Converser, timeout time.Duration) (bool, error) {
	now := time.Now().UTC()
	if err := s.DB.WithContext(ctx).Model(&AgentRun{}).
		Where("status IN ? AND deadline_at <= ?", []string{RunPending, RunRunning}, now).
		Updates(map[string]any{"status": RunFailed, "finished_at": now, "deadline_at": nil, "error_message": "回复超时或服务中断，请重试"}).Error; err != nil {
		return false, err
	}

	terminal := s.DB.Model(&AgentRun{}).Select("id").Where("status IN ?", []string{RunFailed, RunCancelled})
	if err := s.DB.WithContext(ctx).Model(&AgentRunStep{}).
		Where("status = ? AND agent_run_id IN (?)", RunRunning, terminal).Update("status", RunFailed).Error; err != nil {
		return false, err
	}

	var run AgentRun
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ?", RunPending).Order("id").Take(&run).Error; err != nil {
			return err
		}

		if err := tx.Model(&run).Updates(map[string]any{
			"status": RunRunning, "started_at": now, "deadline_at": now.Add(timeout + 10*time.Second),
		}).Error; err != nil {
			return err
		}

		return tx.Create(&AgentRunStep{AgentRunID: run.ID, SequenceNo: 1, Kind: "model", Name: "respond", Status: RunRunning}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if run.Model != s.LLMModel || run.PromptVersion != llm.DialogueVersion || !json.Valid(run.ContextSnapshot) {
		return true, s.failDialogue(run.ID, "模型配置或上下文已变化，请重试")
	}

	if err := s.dialogueActive(ctx, run); err != nil {
		return true, s.failDialogue(run.ID, "当前会话不可用")
	}

	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-requestCtx.Done():
				return
			case <-ticker.C:
				if s.dialogueActive(requestCtx, run) != nil {
					cancel()
					return
				}
			}
		}
	}()
	var response llm.DialogueResponse
	var callErr error
	if recorded, ok := model.(interface {
		ConverseRecorded(context.Context, json.RawMessage, string, agent.Record) (llm.DialogueResponse, error)
	}); ok {
		response, callErr = recorded.ConverseRecorded(requestCtx, run.ContextSnapshot, "turn:"+strconv.FormatUint(run.ID, 10), s.dialogueRecorder(run))
	} else {
		response, callErr = model.Converse(requestCtx, run.ContextSnapshot)
	}
	cancel()
	<-done
	if callErr != nil {
		code, message := llm.FailureDetails(callErr)
		slog.Warn("conversation generation failed", "run_id", run.ID, "failure_code", code)
		return true, s.failDialogue(run.ID, message)
	}

	if err := response.Result.Validate(); err != nil || len(response.ProviderID) > 255 {
		return true, s.failDialogue(run.ID, "模型回复校验失败，请重试")
	}

	if err := s.completeDialogue(ctx, run, response); err != nil {
		return true, s.failDialogue(run.ID, "回复未保存或对话已更新，请重试")
	}

	return true, nil
}

func (s *Store) dialogueActive(ctx context.Context, run AgentRun) error {
	var count int64
	err := s.DB.WithContext(ctx).Table("agent_runs AS r").
		Joins("JOIN conversations AS c ON c.id = r.conversation_id").
		Joins("JOIN agent_users AS u ON u.id = c.user_id").
		Where("r.id = ? AND r.status = ? AND r.deadline_at > ? AND u.status = ?", run.ID, RunRunning, time.Now().UTC(), "active").Count(&count).Error
	if err != nil {
		return err
	}

	if count != 1 {
		return ErrTaskChanged
	}

	return nil
}

func (s *Store) failDialogue(id uint64, message string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&AgentRun{}).Where("id = ? AND status = ?", id, RunRunning).Updates(map[string]any{
			"status": RunFailed, "finished_at": time.Now().UTC(), "deadline_at": nil, "error_message": message,
		}).Error; err != nil {
			return err
		}

		return tx.Model(&AgentRunStep{}).Where("agent_run_id = ? AND status = ?", id, RunRunning).Update("status", RunFailed).Error
	})
}

func (s *Store) completeDialogue(ctx context.Context, run AgentRun, response llm.DialogueResponse) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conv Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conv, run.ConversationID).Error; err != nil {
			return err
		}

		var current AgentRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, run.ID).Error; err != nil {
			return err
		}

		if err := (&Store{DB: tx}).dialogueActive(ctx, current); err != nil {
			return err
		}

		requestID, err := messageUUID()
		if err != nil {
			return err
		}

		message := Message{ConversationID: conv.ID, RequestID: requestID, Role: "assistant", Content: response.Result.Reply}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}

		status := RunSucceeded
		if response.Result.WaitingForUser {
			status = RunWaiting
		}

		result, err := json.Marshal(dialogueSaved{DialogueResult: response.Result, Evidence: response.Evidence})
		if err != nil {
			return err
		}

		if err := tx.Model(&current).Updates(map[string]any{
			"status": status, "result": string(result), "assistant_message_id": message.ID,
			"finished_at": time.Now().UTC(), "deadline_at": nil,
		}).Error; err != nil {
			return err
		}

		if !response.StepsRecorded {
			if err := tx.Model(&AgentRunStep{}).Where("agent_run_id = ? AND sequence_no = ?", run.ID, 1).Updates(map[string]any{
				"status": RunSucceeded, "output": string(result), "provider_response_id": response.ProviderID,
				"input_tokens": response.InputTokens, "output_tokens": response.OutputTokens,
			}).Error; err != nil {
				return err
			}
		}

		return tx.Model(&conv).Updates(map[string]any{
			"context_summary": response.Result.ContextSummary, "summary_through_message_id": message.ID,
			"last_message_at": message.CreatedAt, "updated_at": message.CreatedAt,
		}).Error
	})
}
