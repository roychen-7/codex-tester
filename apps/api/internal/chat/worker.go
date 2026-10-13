package chat

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Work uses a durable MySQL queue. One worker per process bounds API concurrency;
// row locking and unique constraints also serialize across multiple processes.
func (s *Store) Work(ctx context.Context, generator llm.Generator, timeout time.Duration) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for ctx.Err() == nil {
		worked, err := s.ProcessOne(ctx, generator, timeout)
		if err != nil && ctx.Err() == nil {
			slog.Error("agent worker storage operation failed")
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

func (s *Store) ProcessOne(ctx context.Context, generator llm.Generator, timeout time.Duration) (bool, error) {
	now := time.Now().UTC()
	err := s.DB.WithContext(ctx).Model(&LLMRun{}).
		Where("status IN ? AND deadline_at <= ?", []string{RunPending, RunRunning}, now).
		Updates(map[string]any{
			"status": RunFailed, "finished_at": now,
			"error_code": "INTERRUPTED", "error_message": "生成超时或服务中断，请重试",
		}).Error
	if err != nil {
		return false, err
	}

	var run LLMRun
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ?", RunPending).Order("id").Take(&run).Error; err != nil {
			return err
		}

		deadline := time.Now().UTC().Add(timeout + 10*time.Second)
		return tx.Model(&run).Updates(map[string]any{
			"status": RunRunning, "started_at": time.Now().UTC(), "deadline_at": deadline,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	var snapshot GenerationContext
	if json.Unmarshal(run.ContextSnapshot, &snapshot) != nil || run.Model != s.LLMModel {
		return true, s.failRun(run.ID, "CONFIG_CHANGED", "模型配置或上下文已变化，请重试")
	}

	var ownerCount int64
	err = s.DB.WithContext(ctx).Table("conversations").
		Joins("JOIN agent_users ON agent_users.id = conversations.user_id").
		Where("conversations.id = ? AND agent_users.status = ?", run.ConversationID, "active").
		Count(&ownerCount).Error
	if err != nil || ownerCount != 1 {
		return true, s.failRun(run.ID, "UNAVAILABLE", "当前会话不可用")
	}

	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	response, callErr := generator.Generate(requestCtx, run.ContextSnapshot)
	cancel()
	if callErr != nil {
		return true, s.failRun(run.ID, "PROVIDER_ERROR", callErr.Error())
	}

	if err := response.Result.Validate(); err != nil {
		return true, s.failRun(run.ID, "INVALID_RESULT", "模型返回内容校验失败，请重试")
	}

	if err := s.completeRun(ctx, run, snapshot, response); err != nil {
		return true, s.failRun(run.ID, "SAVE_FAILED", "回复未能保存或需求状态已变化，请重试")
	}

	return true, nil
}

func (s *Store) failRun(id uint64, code, message string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.DB.WithContext(ctx).Model(&LLMRun{}).
		Where("id = ? AND status = ?", id, RunRunning).
		Updates(map[string]any{
			"status": RunFailed, "finished_at": time.Now().UTC(),
			"error_code": code, "error_message": message,
		}).Error
}

func (s *Store) completeRun(ctx context.Context, run LLMRun, snapshot GenerationContext, response llm.Response) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conversation Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conversation, run.ConversationID).Error; err != nil {
			return err
		}

		var current LLMRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, run.ID).Error; err != nil {
			return err
		}

		if current.Status != RunRunning || current.DeadlineAt == nil || !current.DeadlineAt.After(time.Now()) || run.TaskID == nil {
			return ErrTaskChanged
		}

		var task AgentTask
		if err := tx.Where("id = ? AND conversation_id = ?", *run.TaskID, conversation.ID).Take(&task).Error; err != nil {
			return err
		}

		if task.Revision != snapshot.TaskRevision || task.Status != TaskClarifying {
			return ErrTaskChanged
		}

		if err := validateSources(tx, conversation.ID, snapshot, response.Result); err != nil {
			return err
		}

		requestID, err := messageUUID()
		if err != nil {
			return err
		}

		message := Message{
			ConversationID: conversation.ID, RequestID: requestID,
			Role: "assistant", Content: response.Result.Reply,
		}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}

		requirement, _ := json.Marshal(response.Result.Requirement)
		missing, _ := json.Marshal(response.Result.MissingInformation)
		result, _ := json.Marshal(response.Result)
		status := TaskClarifying
		if response.Result.ReadyForConfirmation {
			status = TaskAwaitingConfirmation
		}

		if err := tx.Model(&task).Updates(map[string]any{
			"requirement": string(requirement), "missing_information": string(missing),
			"revision": task.Revision + 1, "status": status, "confirmed_at": nil,
		}).Error; err != nil {
			return err
		}

		if err := tx.Model(&conversation).Updates(map[string]any{
			"context_summary":            response.Result.ContextSummary,
			"summary_through_message_id": run.TriggerMessageID,
			"last_message_at":            message.CreatedAt, "updated_at": message.CreatedAt,
		}).Error; err != nil {
			return err
		}

		return tx.Model(&current).Updates(map[string]any{
			"status": RunSucceeded, "finished_at": time.Now().UTC(),
			"result": string(result), "assistant_message_id": message.ID,
			"provider_response_id": response.ProviderID,
			"input_tokens":         response.InputTokens, "output_tokens": response.OutputTokens,
		}).Error
	})
}

func validateSources(tx *gorm.DB, conversationID uint64, snapshot GenerationContext, result llm.Result) error {
	allowed := map[string]bool{}
	for _, message := range snapshot.Messages {
		allowed[strconv.FormatUint(message.ID, 10)] = true
	}

	var prior llm.Requirement
	if err := json.Unmarshal(snapshot.Requirement, &prior); err != nil {
		return ErrInput
	}

	for _, detail := range prior.Details {
		for _, id := range detail.SourceMessageIDs {
			allowed[id] = true
		}
	}

	for _, detail := range result.Requirement.Details {
		for _, id := range detail.SourceMessageIDs {
			if !ValidID(id) || !allowed[id] {
				return ErrInput
			}

			var message Message
			if err := tx.Where("id = ? AND conversation_id = ?", id, conversationID).Take(&message).Error; err != nil {
				return err
			}

			if detail.Source == "user" && message.Role != "user" {
				return ErrInput
			}
		}
	}

	return nil
}

func messageUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}

	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), nil
}
