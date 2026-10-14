package chat

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The existing versioned run JSON stores bounded evidence atomically with its
// assistant message. No mutable tickets or credentials are saved here.
type dialogueSaved struct {
	llm.DialogueResult
	Evidence json.RawMessage `json:"evidence"`
}

func dialogueHistory(tx *gorm.DB, conversation uint64) ([]agent.Evidence, error) {
	var runs []AgentRun
	err := tx.Select("result").Where(
		"conversation_id = ? AND status IN ? AND JSON_TYPE(JSON_EXTRACT(result, '$.evidence')) = 'ARRAY' AND JSON_LENGTH(result, '$.evidence') > 0",
		conversation, []string{RunSucceeded, RunWaiting},
	).Order("id DESC").Limit(4).Find(&runs).Error
	if err != nil {
		return nil, err
	}

	evidence := []agent.Evidence{}
	seen := map[string]bool{}
	for _, run := range runs {
		var saved dialogueSaved
		if json.Unmarshal(run.Result, &saved) != nil {
			return nil, errors.New("已保存的对话结果无效")
		}
		var items []agent.Evidence
		if json.Unmarshal(saved.Evidence, &items) != nil {
			return nil, errors.New("已保存的查询依据无效")
		}
		for _, e := range items {
			if !seen[e.Result.ResultID] {
				evidence = append(evidence, e)
				seen[e.Result.ResultID] = true
			}
			if len(evidence) == 3 {
				return evidence, nil
			}
		}
	}
	return evidence, nil
}

func (s *Store) DialogueEvidence(ctx context.Context, user uint64, conversation, message string) (agent.Outcome, error) {
	out := agent.Outcome{Evidence: []agent.Evidence{}}
	if !ValidID(message) {
		return out, ErrInput
	}
	if _, err := s.Conversation(ctx, user, conversation); err != nil {
		return out, err
	}

	var run AgentRun
	err := s.DB.WithContext(ctx).Where(
		"conversation_id = ? AND assistant_message_id = ? AND status IN ?",
		conversation, message, []string{RunSucceeded, RunWaiting},
	).Take(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}

	var saved dialogueSaved
	if err := json.Unmarshal(run.Result, &saved); err != nil {
		return out, err
	}
	out.Answer = agent.Answer{Reply: saved.Reply, Questions: []string{}, ResultIDs: saved.ResultIDs}
	if out.Answer.ResultIDs == nil {
		out.Answer.ResultIDs = []string{}
	}
	if len(saved.Evidence) > 0 && string(saved.Evidence) != "null" {
		if err := json.Unmarshal(saved.Evidence, &out.Evidence); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *Store) annotateEvidence(ctx context.Context, conversation string, messages []Message) error {
	ids := []uint64{}
	for _, m := range messages {
		if m.Role == "assistant" {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	var matches []uint64
	if err := s.DB.WithContext(ctx).Model(&AgentRun{}).Where(
		"conversation_id = ? AND assistant_message_id IN ? AND JSON_TYPE(JSON_EXTRACT(result, '$.evidence')) = 'ARRAY' AND JSON_LENGTH(result, '$.evidence') > 0",
		conversation, ids,
	).Pluck("assistant_message_id", &matches).Error; err != nil {
		return err
	}
	available := map[uint64]bool{}
	for _, id := range matches {
		available[id] = true
	}
	for i := range messages {
		messages[i].HasEvidence = available[messages[i].ID]
	}
	return nil
}

func (s *Store) dialogueRecorder(run AgentRun) agent.Record {
	return func(ctx context.Context, event agent.Event) error {
		return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current AgentRun
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, run.ID).Error; err != nil {
				return err
			}
			if err := (&Store{DB: tx}).dialogueActive(ctx, current); err != nil {
				return err
			}

			output, err := json.Marshal(map[string]any{
				"input": event.Input, "result": event.Output, "skill_version": agent.SkillVersion(),
			})
			if err != nil {
				return err
			}
			step := AgentRunStep{
				AgentRunID: run.ID, SequenceNo: uint32(event.Sequence), Kind: event.Kind,
				Name: event.Name, Status: event.Status, Output: output,
				InputTokens: event.InputTokens, OutputTokens: event.OutputTokens,
			}
			if event.ProviderID != "" {
				step.ProviderResponseID = &event.ProviderID
			}
			return tx.Clauses(clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{
				"kind", "name", "status", "output", "provider_response_id", "input_tokens", "output_tokens", "updated_at",
			})}).Create(&step).Error
		})
	}
}
