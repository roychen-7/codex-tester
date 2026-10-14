package chat

import (
	"encoding/json"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
)

const RunWaiting = "waiting_for_user"

type AgentRun struct {
	ID                 uint64          `json:"id,string" gorm:"primaryKey"`
	ConversationID     uint64          `json:"conversation_id,string"`
	TriggerMessageID   uint64          `json:"trigger_message_id,string"`
	AttemptNo          uint32          `json:"attempt_no"`
	Status             string          `json:"status"`
	Model              string          `json:"-"`
	PromptVersion      string          `json:"-"`
	ContextSnapshot    json.RawMessage `json:"-" gorm:"serializer:json;type:json"`
	Result             json.RawMessage `json:"-" gorm:"serializer:json;type:json"`
	AssistantMessageID *uint64         `json:"assistant_message_id,string"`
	ErrorMessage       *string         `json:"error_message"`
	StartedAt          *time.Time      `json:"started_at"`
	FinishedAt         *time.Time      `json:"finished_at"`
	DeadlineAt         *time.Time      `json:"deadline_at"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type AgentRunStep struct {
	ID                 uint64 `gorm:"primaryKey"`
	AgentRunID         uint64
	SequenceNo         uint32
	Kind               string
	Name               string
	Status             string
	Output             json.RawMessage `gorm:"serializer:json;type:json"`
	ProviderResponseID *string
	InputTokens        *uint64
	OutputTokens       *uint64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type DialogueContext struct {
	Evidence         []agent.Evidence `json:"evidence"`
	SkillVersion     string           `json:"skill_version"`
	CurrentTime      string           `json:"current_time"`
	Summary          string           `json:"summary"`
	Messages         []Message        `json:"messages"`
	HistoryTruncated bool             `json:"history_truncated"`
}
