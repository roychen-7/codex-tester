package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
)

// Source is a bounded, persisted piece of evidence, not an instruction.
// Both historical messages and web pages use the same envelope.
type Source struct {
	URL            string `json:"url,omitempty"`
	PublishedAt    string `json:"published_at,omitempty"`
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Title          string `json:"title"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Role           string `json:"role"`
	CreatedAt      string `json:"created_at"`
	RetrievedAt    string `json:"retrieved_at"`
	Content        string `json:"content"`
	Truncated      bool   `json:"truncated"`
}

type HistorySearch struct {
	Query    string `json:"query"`
	BeforeID string `json:"before_id"`
}

type HistoryMatches struct {
	Items      []Source `json:"items"`
	NextCursor string   `json:"next_cursor"`
	Coverage   string   `json:"coverage"`
}

// History implementations bind the authenticated owner outside model arguments.
type History interface {
	Search(context.Context, HistorySearch) (HistoryMatches, error)
	Read(context.Context, string) ([]Source, error)
}

func historyTools() []llm.Tool {
	makeTool := func(name, description string, fields []string) llm.Tool {
		properties := map[string]any{}
		for _, field := range fields {
			properties[field] = map[string]string{"type": "string"}
		}

		return llm.Tool{Type: "function", Name: name, Description: description, Strict: true, Parameters: map[string]any{"type": "object", "properties": properties, "required": fields, "additionalProperties": false}}
	}

	return []llm.Tool{
		makeTool("search_history", "搜索自己跨会话的历史消息；query 为一个关键词或短语，before_id 首次为空，后续传 next_cursor。只覆盖最近2000条，最多返回10条摘要；引用前必须 read_history。", []string{"query", "before_id"}),
		makeTool("read_history", "读取本人历史消息及相邻上下文，返回可引用的来源ID和原文（长内容标记截断）", []string{"message_id"}),
	}
}

func callHistory(ctx context.Context, history History, name, arguments string) (json.RawMessage, []Source, error) {
	if history == nil {
		return nil, nil, errors.New("历史检索不可用")
	}

	if name == "search_history" {
		var args HistorySearch
		if err := decode([]byte(arguments), &args); err != nil {
			return nil, nil, err
		}

		matches, err := history.Search(ctx, args)
		return raw(matches), nil, err
	}

	var args struct {
		MessageID string `json:"message_id"`
	}

	if err := decode([]byte(arguments), &args); err != nil {
		return nil, nil, err
	}

	sources, err := history.Read(ctx, args.MessageID)
	return raw(map[string]any{"sources": sources}), sources, err
}
