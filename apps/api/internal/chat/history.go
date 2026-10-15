package chat

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"gorm.io/gorm"
)

// Bound to the run's conversation, never to a user ID supplied by the model.
type runHistory struct {
	store        *Store
	conversation uint64
	cutoff       uint64
}

func (h runHistory) owned(ctx context.Context) *gorm.DB {
	owner := h.store.DB.WithContext(ctx).Model(&Conversation{}).Select("user_id").Where("id = ?", h.conversation)
	conversations := h.store.DB.WithContext(ctx).Model(&Conversation{}).Select("id").Where("user_id IN (?)", owner)
	return h.store.DB.WithContext(ctx).Model(&Message{}).Where("conversation_id IN (?) AND id < ?", conversations, h.cutoff)
}

func (h runHistory) Search(ctx context.Context, args agent.HistorySearch) (agent.HistoryMatches, error) {
	out := agent.HistoryMatches{Items: []agent.Source{}, Coverage: "仅检索当前用户在本轮之前最近2000条消息，按消息ID倒序；关键词未命中不代表历史中不存在。"}
	query := strings.TrimSpace(args.Query)
	if query == "" || utf8.RuneCountInString(query) > 100 || (args.BeforeID != "" && !ValidID(args.BeforeID)) {
		return out, ErrInput
	}

	var ids []uint64
	if err := h.owned(ctx).Order("id DESC").Limit(2000).Pluck("id", &ids).Error; err != nil {
		return out, errors.New("历史检索暂不可用")
	}

	if len(ids) == 0 {
		return out, nil
	}
	// Escape LIKE wildcard characters using an explicit escape character.
	pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(query) + "%"
	q := h.owned(ctx).Where("id >= ? AND content LIKE ? ESCAPE '!'", ids[len(ids)-1], pattern)
	if args.BeforeID != "" {
		q = q.Where("id < ?", args.BeforeID)
	}

	var messages []Message
	if err := q.Order("id DESC").Limit(11).Find(&messages).Error; err != nil {
		return out, errors.New("历史检索暂不可用")
	}

	if len(messages) > 10 {
		messages = messages[:10]
		out.NextCursor = strconv.FormatUint(messages[9].ID, 10)
	}

	for _, m := range messages {
		out.Items = append(out.Items, historySource(m, 240))
	}

	return out, nil
}

func (h runHistory) Read(ctx context.Context, id string) ([]agent.Source, error) {
	if !ValidID(id) {
		return nil, ErrInput
	}

	var target Message
	if err := h.owned(ctx).Where("id = ?", id).Take(&target).Error; err != nil {
		return nil, errors.New("历史消息不可用")
	}

	var before, after []Message
	if err := h.owned(ctx).Where("conversation_id = ? AND id < ?", target.ConversationID, target.ID).Order("id DESC").Limit(2).Find(&before).Error; err != nil {
		return nil, errors.New("历史消息不可用")
	}

	if err := h.owned(ctx).Where("conversation_id = ? AND id > ?", target.ConversationID, target.ID).Order("id ASC").Limit(2).Find(&after).Error; err != nil {
		return nil, errors.New("历史消息不可用")
	}

	out := []agent.Source{}
	for i := len(before) - 1; i >= 0; i-- {
		out = append(out, historySource(before[i], 1000))
	}

	out = append(out, historySource(target, 4000))
	for _, m := range after {
		out = append(out, historySource(m, 1000))
	}

	return out, nil
}

func historySource(m Message, limit int) agent.Source {
	content := []rune(m.Content)
	truncated := len(content) > limit
	if truncated {
		content = content[:limit]
	}

	id := strconv.FormatUint(m.ID, 10)
	return agent.Source{
		ID: "history:" + id, Kind: "history", Title: "历史消息 #" + id,
		ConversationID: strconv.FormatUint(m.ConversationID, 10), MessageID: id, Role: m.Role,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339Nano), RetrievedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Content: string(content), Truncated: truncated,
	}
}
