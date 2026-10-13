package chat

import (
	"encoding/json"
	"time"
)

const (
	TaskClarifying           = "clarifying"
	TaskAwaitingConfirmation = "awaiting_confirmation"
	TaskConfirmed            = "confirmed"
	TaskCancelled            = "cancelled"

	RunPending   = "pending"
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
)

// AgentTask is the current requirement revision, not an executed analysis.
// JSON fields are validated by the service before writes; SQL also checks their shape.
type AgentTask struct {
	ID                 uint64          `json:"id,string" gorm:"primaryKey"`
	ConversationID     uint64          `json:"conversation_id,string"`
	OriginMessageID    uint64          `json:"origin_message_id,string"`
	Status             string          `json:"status"`
	Requirement        json.RawMessage `json:"requirement" gorm:"serializer:json;type:json"`
	MissingInformation json.RawMessage `json:"missing_information" gorm:"serializer:json;type:json"`
	Revision           uint32          `json:"revision"`
	ConfirmedAt        *time.Time      `json:"confirmed_at"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

// LLMRun describes one attempt. Generated constraint columns are deliberately
// absent so GORM never writes them. Provider retries must create a new attempt.
type LLMRun struct {
	ID                 uint64          `json:"id,string" gorm:"primaryKey"`
	ConversationID     uint64          `json:"conversation_id,string"`
	TaskID             *uint64         `json:"task_id,string"`
	TriggerMessageID   uint64          `json:"trigger_message_id,string"`
	AttemptNo          uint32          `json:"attempt_no"`
	Status             string          `json:"status"`
	Model              string          `json:"model"`
	PromptVersion      string          `json:"prompt_version"`
	ProviderResponseID *string         `json:"-"`
	ContextSnapshot    json.RawMessage `json:"-" gorm:"serializer:json;type:json"`
	Result             json.RawMessage `json:"-" gorm:"serializer:json;type:json"`
	AssistantMessageID *uint64         `json:"assistant_message_id,string"`
	InputTokens        *uint64         `json:"input_tokens"`
	OutputTokens       *uint64         `json:"output_tokens"`
	ErrorCode          *string         `json:"error_code"`
	ErrorMessage       *string         `json:"error_message"`
	StartedAt          *time.Time      `json:"started_at"`
	FinishedAt         *time.Time      `json:"finished_at"`
	DeadlineAt         *time.Time      `json:"deadline_at"`
	CreatedAt          time.Time       `json:"created_at"`
}

func (LLMRun) TableName() string {
	return "llm_runs"
}
