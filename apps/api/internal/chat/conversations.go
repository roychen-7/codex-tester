package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
)

func (s *Store) Conversation(ctx context.Context, user uint64, id string) (Conversation, error) {
	var c Conversation
	err := s.DB.WithContext(ctx).Where("id = ? AND user_id = ?", id, user).Take(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}

	return c, err
}

func (s *Store) CreateConversation(ctx context.Context, user uint64, title string) (Conversation, error) {
	title, err := Title(title)
	if err != nil {
		return Conversation{}, err
	}

	c := Conversation{UserID: user, Title: title}
	err = s.DB.WithContext(ctx).Create(&c).Error
	return c, err
}

func (s *Store) Rename(ctx context.Context, user uint64, id, title string) (Conversation, error) {
	title, err := Title(title)
	if err != nil {
		return Conversation{}, err
	}

	err = s.DB.WithContext(ctx).
		Model(&Conversation{}).
		Where("id = ? AND user_id = ?", id, user).
		Update("title", title).Error
	if err != nil {
		return Conversation{}, err
	}

	return s.Conversation(ctx, user, id)
}

func (s *Store) Conversations(ctx context.Context, user uint64, cursor string, limit int) (Page[Conversation], error) {
	p := Page[Conversation]{Items: []Conversation{}}
	if limit < 1 || limit > 100 {
		return p, ErrInput
	}

	query := s.DB.WithContext(ctx).Where("user_id = ?", user)
	if cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		var c Cursor
		if e != nil || json.Unmarshal(raw, &c) != nil || c.ID == 0 || c.Time.IsZero() {
			return p, ErrInput
		}

		query = query.Where("updated_at < ? OR (updated_at = ? AND id < ?)", c.Time.UTC(), c.Time.UTC(), c.ID)
	}

	err := query.Order("updated_at DESC").Order("id DESC").Limit(limit + 1).Find(&p.Items).Error
	if err != nil {
		return p, err
	}

	if len(p.Items) > limit {
		p.Items = p.Items[:limit]
		last := p.Items[limit-1]
		raw, _ := json.Marshal(Cursor{last.UpdatedAt, last.ID})
		p.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}

	return p, nil
}
