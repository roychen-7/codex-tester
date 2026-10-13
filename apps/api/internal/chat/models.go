package chat

import (
	"time"
)

type User struct {
	ID          uint64 `json:"id,string" gorm:"primaryKey"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

type Account struct {
	User         `gorm:"embedded"`
	PasswordHash string    `json:"-"`
	Status       string    `json:"-"`
	CreatedAt    time.Time `json:"-"`
	UpdatedAt    time.Time `json:"-"`
}

func (Account) TableName() string {
	return "agent_users"
}

type Session struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64
	TokenHash []byte
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

func (Session) TableName() string {
	return "auth_sessions"
}

type Conversation struct {
	ID                      uint64     `json:"id,string" gorm:"primaryKey"`
	UserID                  uint64     `json:"-"`
	Title                   string     `json:"title"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	LastMessageAt           *time.Time `json:"last_message_at"`
	ContextSummary          *string    `json:"-"`
	SummaryThroughMessageID *uint64    `json:"-"`
}

type Message struct {
	ID             uint64    `json:"id,string" gorm:"primaryKey"`
	ConversationID uint64    `json:"conversation_id,string"`
	RequestID      string    `json:"request_id"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

type Cursor struct {
	Time time.Time `json:"time"`
	ID   uint64    `json:"id,string"`
}
