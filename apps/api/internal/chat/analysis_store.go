package chat

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"gorm.io/gorm"
)

// queueAnalysis requires the conversation lock. A confirmed revision starts once;
// explicit retry is the only way to create another attempt of that revision.
func (s *Store) queueAnalysis(tx *gorm.DB, task AgentTask, retry bool) (AnalysisRun, error) {
	var previous AnalysisRun
	err := tx.Where("task_id = ? AND task_revision = ?", task.ID, task.Revision).Order("attempt_no DESC").Take(&previous).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return previous, err
	}

	if err == nil && (!retry || previous.Status == RunPending || previous.Status == RunRunning) {
		return previous, nil
	}

	if retry && (err != nil || (previous.Status != RunFailed && previous.Status != RunCancelled)) {
		return previous, ErrTaskChanged
	}

	run := AnalysisRun{
		ConversationID: task.ConversationID, TaskID: task.ID, TaskRevision: task.Revision,
		AttemptNo: previous.AttemptNo + 1, Status: RunPending, Phase: "queued",
		Model: s.LLMModel, SkillVersion: agent.SkillVersion(), ContextSnapshot: task.Requirement,
		DeadlineAt: time.Now().UTC().Add(5 * time.Minute),
	}
	err = tx.Create(&run).Error
	return run, err
}

func (s *Store) StartAnalysis(ctx context.Context, user uint64, id, taskID string, revision uint32, retry bool) (AnalysisRun, error) {
	var run AnalysisRun
	if !s.AnalysisEnabled || s.ConversationMode {
		return run, ErrLLMDisabled
	}

	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conv, err := ownedConversation(tx, user, id)
		if err != nil {
			return err
		}

		var task AgentTask
		if err := tx.Where("id = ? AND conversation_id = ?", taskID, conv.ID).Take(&task).Error; err != nil {
			return ErrNotFound
		}

		if task.Status != TaskConfirmed || task.Revision != revision {
			return ErrTaskChanged
		}

		busy, err := activeRun(tx, conv.ID)
		if err != nil {
			return err
		}

		if busy {
			return ErrBusy
		}

		run, err = s.queueAnalysis(tx, task, retry)
		return err
	})
	return run, err
}

func (s *Store) CancelAnalysis(ctx context.Context, user uint64, id, runID string) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conv, err := ownedConversation(tx, user, id)
		if err != nil {
			return err
		}

		var run AnalysisRun
		if err := tx.Where("id = ? AND conversation_id = ?", runID, conv.ID).Take(&run).Error; err != nil {
			return ErrNotFound
		}

		return tx.Model(&AnalysisRun{}).Where("id = ? AND status IN ?", run.ID, []string{RunPending, RunRunning}).Updates(map[string]any{"status": RunCancelled, "phase": "cancelled", "finished_at": time.Now().UTC()}).Error
	})
}

func (s *Store) AnalysisResult(ctx context.Context, user uint64, id, runID string) (agent.Outcome, error) {
	var out agent.Outcome
	if _, err := s.Conversation(ctx, user, id); err != nil {
		return out, err
	}

	var run AnalysisRun
	if err := s.DB.WithContext(ctx).Where("id = ? AND conversation_id = ?", runID, id).Take(&run).Error; err != nil {
		return out, ErrNotFound
	}

	if run.Status != RunSucceeded {
		return out, ErrTaskChanged
	}

	if err := json.Unmarshal(run.Answer, &out.Answer); err != nil {
		return out, err
	}

	var results []QueryResult
	if err := s.DB.WithContext(ctx).Where("analysis_run_id = ?", run.ID).Order("id").Find(&results).Error; err != nil {
		return out, err
	}

	out.Evidence = []agent.Evidence{}
	for _, result := range results {
		var evidence agent.Evidence
		if err := json.Unmarshal(result.Evidence, &evidence); err != nil {
			return out, err
		}

		out.Evidence = append(out.Evidence, evidence)
	}

	return out, nil
}
