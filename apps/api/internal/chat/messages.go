package chat

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) Messages(ctx context.Context, user uint64, id, before string, limit int) (Page[Message], error) {
	p := Page[Message]{Items: []Message{}}
	if limit < 1 || limit > 100 || (before != "" && !ValidID(before)) {
		return p, ErrInput
	}

	if _, err := s.Conversation(ctx, user, id); err != nil {
		return p, err
	}

	q := s.DB.WithContext(ctx).Where("conversation_id = ?", id)
	if before != "" {
		q = q.Where("id < ?", before)
	}

	err := q.Order("id DESC").Limit(limit + 1).Find(&p.Items).Error
	if err != nil {
		return p, err
	}

	if len(p.Items) > limit {
		p.Items = p.Items[:limit]
		p.NextCursor = strconv.FormatUint(p.Items[limit-1].ID, 10)
	}

	return p, nil
}

func (s *Store) Send(ctx context.Context, user uint64, id, requestID, content string) (Message, bool, error) {
	if !uuidPattern.MatchString(requestID) ||
		!utf8.ValidString(content) ||
		strings.TrimSpace(content) == "" ||
		utf8.RuneCountInString(content) > 10000 {
		return Message{}, false, ErrInput
	}

	var m Message
	created := false
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c Conversation
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", id, user).
			Take(&c).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}

		if err != nil {
			return err
		}

		err = tx.Where("conversation_id = ? AND request_id = ?", c.ID, requestID).Take(&m).Error
		if err == nil {
			if m.Content != content || m.Role != "user" {
				return ErrConflict
			}

			return nil
		}

		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		m = Message{
			ConversationID: c.ID,
			RequestID:      requestID,
			Role:           "user",
			Content:        content,
		}
		if err = tx.Create(&m).Error; err != nil {
			return err
		}

		if err = tx.Model(&c).
			Updates(map[string]any{
				"last_message_at": m.CreatedAt,
				"updated_at":      m.CreatedAt,
			}).Error; err != nil {
			return err
		}

		created = true
		return nil
	})

	return m, created, err
}
