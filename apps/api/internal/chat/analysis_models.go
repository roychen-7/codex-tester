package chat

import (
	"encoding/json"
	"time"
)

type AnalysisRun struct {
	ID                 uint64          `json:"id,string" gorm:"primaryKey"`
	ConversationID     uint64          `json:"conversation_id,string"`
	TaskID             uint64          `json:"task_id,string"`
	TaskRevision       uint32          `json:"task_revision"`
	AttemptNo          uint32          `json:"attempt_no"`
	Status             string          `json:"status"`
	Phase              string          `json:"phase"`
	Model              string          `json:"-"`
	SkillVersion       string          `json:"skill_version"`
	ContextSnapshot    json.RawMessage `json:"-" gorm:"serializer:json;type:json"`
	Answer             json.RawMessage `json:"-" gorm:"serializer:json;type:json"`
	AssistantMessageID *uint64         `json:"assistant_message_id,string"`
	ErrorMessage       *string         `json:"error_message"`
	StartedAt          *time.Time      `json:"started_at"`
	FinishedAt         *time.Time      `json:"finished_at"`
	DeadlineAt         time.Time       `json:"deadline_at"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type AnalysisStep struct {
	ID                 uint64 `gorm:"primaryKey"`
	AnalysisRunID      uint64
	SequenceNo         int
	Kind               string
	Name               string
	Status             string
	Input              json.RawMessage `gorm:"serializer:json;type:json"`
	Output             json.RawMessage `gorm:"serializer:json;type:json"`
	ProviderResponseID *string
	InputTokens        *uint64
	OutputTokens       *uint64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type QueryResult struct {
	ID            uint64 `gorm:"primaryKey"`
	AnalysisRunID uint64
	ResultID      string
	Evidence      json.RawMessage `gorm:"serializer:json;type:json"`
	CreatedAt     time.Time
}
