package chat

import (
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"gorm.io/gorm"
)

var (
	ErrBusy        = errors.New("Agent 正在处理当前会话，请等待回复后再发送")
	ErrTaskChanged = errors.New("需求或调用状态已变化，请刷新后重试")
	ErrLLMDisabled = errors.New("尚未配置模型服务")
)

type GenerationContext struct {
	CurrentTime      string          `json:"current_time"`
	Summary          string          `json:"summary"`
	TaskRevision     uint32          `json:"task_revision"`
	Requirement      json.RawMessage `json:"requirement"`
	Messages         []Message       `json:"messages"`
	HistoryTruncated bool            `json:"history_truncated"`
}

func activeRun(tx *gorm.DB, conversationID uint64) (bool, error) {
	var count int64
	err := tx.Model(&LLMRun{}).
		Where("conversation_id = ? AND status IN ?", conversationID, []string{RunPending, RunRunning}).
		Count(&count).Error
	return count != 0, err
}

// enqueue runs under the conversation row lock and the message transaction.
func (s *Store) enqueue(tx *gorm.DB, conversation Conversation, message Message) error {
	var task AgentTask
	err := tx.Where("conversation_id = ? AND status <> ?", conversation.ID, TaskCancelled).
		Order("id DESC").Take(&task).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		task = AgentTask{
			ConversationID:     conversation.ID,
			OriginMessageID:    message.ID,
			Status:             TaskClarifying,
			Requirement:        json.RawMessage(`{"goal":"","details":[]}`),
			MissingInformation: json.RawMessage(`[]`),
			Revision:           1,
		}
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
	} else {
		task.Revision++
		task.Status = TaskClarifying
		task.ConfirmedAt = nil
		if err := tx.Model(&task).Updates(map[string]any{
			"revision": task.Revision, "status": TaskClarifying, "confirmed_at": nil,
		}).Error; err != nil {
			return err
		}
	}

	var messages []Message
	if err := tx.Where("conversation_id = ? AND id <= ?", conversation.ID, message.ID).
		Order("id DESC").Limit(33).Find(&messages).Error; err != nil {
		return err
	}

	context := GenerationContext{
		CurrentTime:  time.Now().UTC().Format(time.RFC3339),
		TaskRevision: task.Revision,
		Requirement:  task.Requirement,
		Messages:     []Message{},
	}
	if conversation.ContextSummary != nil {
		context.Summary = *conversation.ContextSummary
	}

	// Keep the latest message in full and bound older text. The requirement and
	// cumulative summary preserve known constraints when older turns are omitted.
	size := 0
	for _, item := range messages {
		if len(context.Messages) == 32 || (len(context.Messages) > 0 && size+len(item.Content) > 40000) {
			context.HistoryTruncated = true
			break
		}

		size += len(item.Content)
		context.Messages = append(context.Messages, item)
	}
	slices.Reverse(context.Messages)

	snapshot, err := json.Marshal(context)
	if err != nil {
		return err
	}

	deadline := time.Now().UTC().Add(5 * time.Minute)
	run := LLMRun{
		ConversationID:   conversation.ID,
		TaskID:           &task.ID,
		TriggerMessageID: message.ID,
		AttemptNo:        1,
		Status:           RunPending,
		Model:            s.LLMModel,
		PromptVersion:    llm.PromptVersion,
		ContextSnapshot:  snapshot,
		DeadlineAt:       &deadline,
	}
	return tx.Create(&run).Error
}
