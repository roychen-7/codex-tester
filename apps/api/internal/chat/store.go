package chat

import "gorm.io/gorm"

// Store persists Agent-owned data. It never connects to the ddshub database.
type Store struct {
	DB                *gorm.DB
	LLMModel          string
	AnalysisEnabled   bool
	QueryToolsEnabled bool
	ConversationMode  bool
}
