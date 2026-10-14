package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) WorkAnalysis(ctx context.Context, model llm.Rounder, cfg config.Agent) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for ctx.Err() == nil {
		if err := s.processAnalysis(ctx, model, cfg); err != nil && ctx.Err() == nil {
			slog.Error("analysis worker storage operation failed")
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Store) processAnalysis(ctx context.Context, model llm.Rounder, cfg config.Agent) error {
	now := time.Now().UTC()
	if err := s.DB.WithContext(ctx).Model(&AnalysisRun{}).
		Where("status IN ? AND deadline_at <= ?", []string{RunPending, RunRunning}, now).
		Updates(map[string]any{
			"status": RunFailed, "phase": "failed", "finished_at": now,
			"error_message": "分析超时或服务中断，请重试",
		}).Error; err != nil {
		return err
	}

	terminal := s.DB.Model(&AnalysisRun{}).Select("id").Where("status IN ?", []string{RunFailed, RunCancelled})
	if err := s.DB.WithContext(ctx).Model(&AnalysisStep{}).
		Where("status = ? AND analysis_run_id IN (?)", RunRunning, terminal).
		Update("status", RunFailed).Error; err != nil {
		return err
	}

	var run AnalysisRun
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ?", RunPending).Order("id").Take(&run).Error; err != nil {
			return err
		}

		return tx.Model(&run).Updates(map[string]any{
			"status": RunRunning, "phase": "connecting",
			"started_at": now, "deadline_at": now.Add(10 * time.Minute),
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-requestCtx.Done():
				return
			case <-ticker.C:
				if s.analysisActive(requestCtx, run) != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		<-done
	}()

	fail := func() error {
		saveCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()

		return s.DB.WithContext(saveCtx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&AnalysisStep{}).Where("analysis_run_id = ? AND status = ?", run.ID, RunRunning).Update("status", RunFailed).Error; err != nil {
				return err
			}

			return tx.Model(&AnalysisRun{}).Where("id = ? AND status = ?", run.ID, RunRunning).
				Updates(map[string]any{
					"status": RunFailed, "phase": "failed", "finished_at": time.Now().UTC(),
					"error_message": "分析未完成：连接、模型或查询失败，或需求已变更。可以重试。",
				}).Error
		})
	}

	if run.Model != s.LLMModel || run.SkillVersion != agent.SkillVersion() || s.analysisActive(requestCtx, run) != nil {
		return fail()
	}

	remote, err := agent.Connect(requestCtx, cfg, fmt.Sprintf("analysis:%d", run.ID))
	if err != nil {
		return fail()
	}
	defer remote.Close()

	outcome, err := agent.Run(requestCtx, model, remote, run.ContextSnapshot, func(ctx context.Context, event agent.Event) error {
		return s.recordAnalysis(ctx, run, event)
	})
	if err != nil {
		return fail()
	}

	if err := s.completeAnalysis(requestCtx, run, outcome); err != nil {
		return fail()
	}

	return nil
}

func (s *Store) analysisActive(ctx context.Context, run AnalysisRun) error {
	var count int64
	err := s.DB.WithContext(ctx).Table("analysis_runs AS r").
		Joins("JOIN agent_tasks AS t ON t.id = r.task_id").
		Joins("JOIN conversations AS c ON c.id = r.conversation_id").
		Joins("JOIN agent_users AS u ON u.id = c.user_id").
		Where("r.id = ? AND r.status = ? AND r.deadline_at > ?", run.ID, RunRunning, time.Now().UTC()).
		Where("t.status = ? AND t.revision = r.task_revision AND u.status = ?", TaskConfirmed, "active").
		Count(&count).Error
	if err != nil {
		return err
	}

	if count != 1 {
		return ErrTaskChanged
	}

	return nil
}

func (s *Store) recordAnalysis(ctx context.Context, run AnalysisRun, event agent.Event) error {
	if err := s.analysisActive(ctx, run); err != nil {
		return err
	}

	if len(event.Name) > 128 || len(event.ProviderID) > 255 {
		return ErrInput
	}

	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		step := AnalysisStep{
			AnalysisRunID: run.ID, SequenceNo: event.Sequence,
			Kind: event.Kind, Name: event.Name, Status: event.Status,
			Input: event.Input, Output: event.Output,
			InputTokens: event.InputTokens, OutputTokens: event.OutputTokens,
		}
		if event.ProviderID != "" {
			step.ProviderResponseID = &event.ProviderID
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "analysis_run_id"}, {Name: "sequence_no"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"status", "output", "provider_response_id", "input_tokens", "output_tokens", "updated_at",
			}),
		}).Create(&step).Error; err != nil {
			return err
		}

		return tx.Model(&AnalysisRun{}).Where("id = ? AND status = ?", run.ID, RunRunning).Update("phase", event.Name).Error
	})
}

func (s *Store) completeAnalysis(ctx context.Context, run AnalysisRun, out agent.Outcome) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conv Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conv, run.ConversationID).Error; err != nil {
			return err
		}

		var current AnalysisRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, run.ID).Error; err != nil {
			return err
		}

		check := &Store{DB: tx}
		if err := check.analysisActive(ctx, run); err != nil {
			return err
		}

		requestID, err := messageUUID()
		if err != nil {
			return err
		}

		message := Message{ConversationID: conv.ID, RequestID: requestID, Role: "assistant", Content: out.Answer.Reply}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}

		for _, evidence := range out.Evidence {
			data, err := json.Marshal(evidence)
			if err != nil {
				return err
			}

			if err := tx.Create(&QueryResult{AnalysisRunID: run.ID, ResultID: evidence.Result.ResultID, Evidence: data}).Error; err != nil {
				return err
			}
		}

		if len(out.Answer.Questions) > 0 {
			questions, _ := json.Marshal(out.Answer.Questions)
			if err := tx.Model(&AgentTask{}).Where("id = ?", run.TaskID).Updates(map[string]any{"status": TaskClarifying, "revision": run.TaskRevision + 1, "missing_information": string(questions), "confirmed_at": nil}).Error; err != nil {
				return err
			}
		}

		if err := tx.Model(&conv).Updates(map[string]any{"last_message_at": message.CreatedAt, "updated_at": message.CreatedAt}).Error; err != nil {
			return err
		}

		answer, _ := json.Marshal(out.Answer)
		return tx.Model(&current).Updates(map[string]any{"status": RunSucceeded, "phase": "completed", "answer": string(answer), "assistant_message_id": message.ID, "finished_at": time.Now().UTC()}).Error
	})
}
