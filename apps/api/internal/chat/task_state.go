package chat

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AgentState struct {
	QueryToolsEnabled bool         `json:"query_tools_enabled"`
	TurnPhase         string       `json:"turn_phase"`
	Mode              string       `json:"mode"`
	Turn              *AgentRun    `json:"turn"`
	Enabled           bool         `json:"enabled"`
	Task              *AgentTask   `json:"task"`
	Run               *LLMRun      `json:"run"`
	AnalysisEnabled   bool         `json:"analysis_enabled"`
	Analysis          *AnalysisRun `json:"analysis"`
}

func (s *Store) AgentState(ctx context.Context, user uint64, id string) (AgentState, error) {
	state := AgentState{Enabled: s.LLMModel != "", AnalysisEnabled: s.AnalysisEnabled, Mode: "legacy"}
	if s.ConversationMode {
		state.Mode = "conversation"
		state.QueryToolsEnabled = s.QueryToolsEnabled
	}
	if _, err := s.Conversation(ctx, user, id); err != nil {
		return state, err
	}

	var turn AgentRun
	err := s.DB.WithContext(ctx).Where("conversation_id = ?", id).Order("id DESC").Take(&turn).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return state, err
	}
	if err == nil {
		state.Turn = &turn
		var step AgentRunStep
		stepErr := s.DB.WithContext(ctx).Where("agent_run_id = ?", turn.ID).Order("sequence_no DESC").Take(&step).Error
		if stepErr != nil && !errors.Is(stepErr, gorm.ErrRecordNotFound) {
			return state, stepErr
		}
		state.TurnPhase = step.Name
	}

	var task AgentTask
	err = s.DB.WithContext(ctx).Where("conversation_id = ?", id).Order("id DESC").Take(&task).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return state, err
	}

	if err == nil {
		state.Task = &task
	}

	var run LLMRun
	err = s.DB.WithContext(ctx).Where("conversation_id = ?", id).Order("id DESC").Take(&run).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return state, err
	}

	if err == nil {
		state.Run = &run
	}

	var analysis AnalysisRun
	err = s.DB.WithContext(ctx).Where("conversation_id = ?", id).Order("id DESC").Take(&analysis).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return state, err
	}
	if err == nil {
		state.Analysis = &analysis
	}
	return state, nil
}

func ownedConversation(tx *gorm.DB, user uint64, id string) (Conversation, error) {
	var conversation Conversation
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND user_id = ?", id, user).Take(&conversation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}

	return conversation, err
}

func (s *Store) Confirm(ctx context.Context, user uint64, id, taskID string, revision uint32) (AgentTask, error) {
	var task AgentTask
	if s.ConversationMode {
		return task, ErrTaskChanged
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conversation, err := ownedConversation(tx, user, id)
		if err != nil {
			return err
		}

		if err := tx.Where("id = ? AND conversation_id = ?", taskID, conversation.ID).Take(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}

			return err
		}

		busy, err := activeRun(tx, conversation.ID)
		if err != nil {
			return err
		}

		if busy || revision != task.Revision || (task.Status != TaskAwaitingConfirmation && task.Status != TaskConfirmed) {
			return ErrTaskChanged
		}

		if task.Status == TaskConfirmed {
			if s.AnalysisEnabled {
				_, err := s.queueAnalysis(tx, task, false)
				return err
			}
			return nil
		}

		now := time.Now().UTC()
		task.Status = TaskConfirmed
		task.ConfirmedAt = &now
		if err := tx.Model(&task).Updates(map[string]any{"status": TaskConfirmed, "confirmed_at": now}).Error; err != nil {
			return err
		}
		if s.AnalysisEnabled {
			_, err := s.queueAnalysis(tx, task, false)
			return err
		}
		return nil
	})
	return task, err
}

// Retry keys the operation by a failed attempt; repeated clicks reuse its successor.
func (s *Store) Retry(ctx context.Context, user uint64, id, runID string) (LLMRun, error) {
	var next LLMRun
	if s.LLMModel == "" || s.ConversationMode {
		return next, ErrLLMDisabled
	}

	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conversation, err := ownedConversation(tx, user, id)
		if err != nil {
			return err
		}

		var previous LLMRun
		if err := tx.Where("id = ? AND conversation_id = ?", runID, conversation.ID).Take(&previous).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}

			return err
		}

		if previous.Status != RunFailed {
			return ErrTaskChanged
		}

		err = tx.Where("trigger_message_id = ? AND attempt_no = ?", previous.TriggerMessageID, previous.AttemptNo+1).Take(&next).Error
		if err == nil {
			return nil
		}

		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		busy, err := activeRun(tx, conversation.ID)
		if err != nil {
			return err
		}

		var latest Message
		if err := tx.Where("conversation_id = ? AND role = ?", conversation.ID, "user").Order("id DESC").Take(&latest).Error; err != nil {
			return err
		}

		if busy || latest.ID != previous.TriggerMessageID {
			return ErrTaskChanged
		}

		deadline := time.Now().UTC().Add(5 * time.Minute)
		next = LLMRun{
			ConversationID: conversation.ID, TaskID: previous.TaskID,
			TriggerMessageID: previous.TriggerMessageID, AttemptNo: previous.AttemptNo + 1,
			Status: RunPending, Model: s.LLMModel, PromptVersion: previous.PromptVersion,
			ContextSnapshot: previous.ContextSnapshot, DeadlineAt: &deadline,
		}
		return tx.Create(&next).Error
	})
	return next, err
}
