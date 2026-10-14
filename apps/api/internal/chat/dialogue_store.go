package chat

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"gorm.io/gorm"
)

// enqueueDialogue is called under the conversation lock, with the user message.
func (s *Store) enqueueDialogue(tx *gorm.DB, conv Conversation, message Message) error {
	now := time.Now().UTC()
	if err := tx.Model(&AgentRun{}).Where("conversation_id = ? AND status IN ?", conv.ID, []string{RunPending, RunRunning}).Updates(map[string]any{
		"status": RunCancelled, "finished_at": now, "deadline_at": nil,
	}).Error; err != nil {
		return err
	}

	// Retire any legacy attempt when switching a conversation to the new runtime.
	if err := tx.Model(&LLMRun{}).Where("conversation_id = ? AND status IN ?", conv.ID, []string{RunPending, RunRunning}).Updates(map[string]any{
		"status": RunCancelled, "finished_at": now,
	}).Error; err != nil {
		return err
	}

	var messages []Message
	if err := tx.Where("conversation_id = ? AND id <= ?", conv.ID, message.ID).Order("id DESC").Limit(33).Find(&messages).Error; err != nil {
		return err
	}

	snapshot := DialogueContext{CurrentTime: now.Format(time.RFC3339), Messages: []Message{}}
	if conv.ContextSummary != nil {
		snapshot.Summary = *conv.ContextSummary
	}

	size := 0
	for _, m := range messages {
		if len(snapshot.Messages) == 32 || (len(snapshot.Messages) > 0 && size+len(m.Content) > 40000) {
			snapshot.HistoryTruncated = true
			break
		}

		snapshot.Messages = append(snapshot.Messages, m)
		size += len(m.Content)
	}

	evidence, err := dialogueHistory(tx, conv.ID)
	if err != nil {
		return err
	}
	snapshot.Evidence = evidence
	snapshot.SkillVersion = agent.SkillVersion()

	slices.Reverse(snapshot.Messages)
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	deadline := now.Add(5 * time.Minute)
	return tx.Create(&AgentRun{
		ConversationID: conv.ID, TriggerMessageID: message.ID, AttemptNo: 1, Status: RunPending,
		Model: s.LLMModel, PromptVersion: llm.DialogueVersion, ContextSnapshot: data, DeadlineAt: &deadline,
	}).Error
}

func (s *Store) CancelDialogue(ctx context.Context, user uint64, id, runID string) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conv, err := ownedConversation(tx, user, id)
		if err != nil {
			return err
		}

		var run AgentRun
		if err := tx.Where("id = ? AND conversation_id = ?", runID, conv.ID).Take(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}

			return err
		}

		if err := tx.Model(&AgentRun{}).Where("id = ? AND status IN ?", run.ID, []string{RunPending, RunRunning}).Updates(map[string]any{
			"status": RunCancelled, "finished_at": time.Now().UTC(), "deadline_at": nil,
		}).Error; err != nil {
			return err
		}

		return tx.Model(&AgentRunStep{}).Where("agent_run_id = ? AND status = ?", run.ID, RunRunning).Update("status", RunCancelled).Error
	})
}

func (s *Store) RetryDialogue(ctx context.Context, user uint64, id, runID string) (AgentRun, error) {
	var next AgentRun
	if !s.ConversationMode || s.LLMModel == "" {
		return next, ErrLLMDisabled
	}

	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conv, err := ownedConversation(tx, user, id)
		if err != nil {
			return err
		}

		var previous AgentRun
		if err := tx.Where("id = ? AND conversation_id = ?", runID, conv.ID).Take(&previous).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}

			return err
		}

		if previous.Status != RunFailed && previous.Status != RunCancelled {
			return ErrTaskChanged
		}

		var latest Message
		if err := tx.Where("conversation_id = ? AND role = ?", conv.ID, "user").Order("id DESC").Take(&latest).Error; err != nil {
			return err
		}

		if latest.ID != previous.TriggerMessageID {
			return ErrTaskChanged
		}

		err = tx.Where("trigger_message_id = ? AND attempt_no = ?", previous.TriggerMessageID, previous.AttemptNo+1).Take(&next).Error
		if err == nil {
			return nil
		}

		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		deadline := time.Now().UTC().Add(5 * time.Minute)
		next = AgentRun{
			ConversationID: conv.ID, TriggerMessageID: previous.TriggerMessageID, AttemptNo: previous.AttemptNo + 1,
			Status: RunPending, Model: s.LLMModel, PromptVersion: llm.DialogueVersion,
			ContextSnapshot: previous.ContextSnapshot, DeadlineAt: &deadline,
		}

		return tx.Create(&next).Error
	})
	return next, err
}
