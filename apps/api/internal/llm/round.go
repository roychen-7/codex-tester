package llm

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrNativeTool = errors.New("upstream returned an unconfigured native tool")

type toolChoiceKey struct{}

// WithToolChoice constrains one recovery request to a declared function or no tools.
func WithToolChoice(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, toolChoiceKey{}, name)
}

type Tool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
	Strict      bool   `json:"strict"`
}

type Call struct {
	ID        string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Round struct {
	Items        []json.RawMessage
	Calls        []Call
	Text         string
	ProviderID   string
	InputTokens  *uint64
	OutputTokens *uint64
}

type Rounder interface {
	Next(context.Context, string, []json.RawMessage, []Tool) (Round, error)
}

func (o *OpenAI) Next(ctx context.Context, instructions string, input []json.RawMessage, tools []Tool) (Round, error) {
	return o.next(ctx, instructions, input, tools, false)
}

// DialogueRounder uses the same Responses tool protocol with a conversational final answer.
type DialogueRounder struct{ Model *OpenAI }

func (d DialogueRounder) Next(ctx context.Context, instructions string, input []json.RawMessage, tools []Tool) (Round, error) {
	return d.Model.next(ctx, instructions, input, tools, true)
}

func (o *OpenAI) next(ctx context.Context, instructions string, input []json.RawMessage, tools []Tool, dialogue bool) (Round, error) {
	text := map[string]any{"type": "string"}
	schema := map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"reply": text, "questions": map[string]any{"type": "array", "items": text}, "result_ids": map[string]any{"type": "array", "items": text}},
		"required":   []string{"reply", "questions", "result_ids"},
	}

	name := "analysis_answer"
	if dialogue {
		name = "conversation_answer"
		properties := schema["properties"].(map[string]any)
		properties["waiting_for_user"] = map[string]string{"type": "boolean"}
		properties["context_summary"] = text
		properties["conclusions"] = conclusionSchema()
		properties["source_ids"] = map[string]any{"type": "array", "items": text}
		schema["required"] = []string{"reply", "questions", "result_ids", "source_ids", "conclusions", "waiting_for_user", "context_summary"}
	}

	payload := map[string]any{
		"model": o.cfg.Model, "instructions": instructions, "input": input, "tools": tools,
		"store": false, "parallel_tool_calls": false, "max_output_tokens": 6000,
		"include": []string{"reasoning.encrypted_content"},
		"text":    map[string]any{"format": map[string]any{"type": "json_schema", "name": name, "strict": true, "schema": schema}},
	}

	if choice, ok := ctx.Value(toolChoiceKey{}).(string); ok {
		if choice == "none" {
			payload["tool_choice"] = "none"
		} else {
			payload["tool_choice"] = map[string]string{"type": "function", "name": choice}
		}
	}

	data, err := o.request(ctx, payload)
	if err != nil {
		return Round{}, err
	}

	var wire struct {
		ID     string            `json:"id"`
		Status string            `json:"status"`
		Output []json.RawMessage `json:"output"`
		Usage  *struct {
			Input  *uint64 `json:"input_tokens"`
			Output *uint64 `json:"output_tokens"`
		} `json:"usage"`
	}

	if json.Unmarshal(data, &wire) != nil || wire.Status != "completed" || len(wire.ID) > 255 {
		return Round{}, requestError("response_incomplete", "模型服务返回了不完整或无效的响应，请重试")
	}

	out := Round{Items: wire.Output, ProviderID: wire.ID}
	if wire.Usage != nil {
		out.InputTokens = wire.Usage.Input
		out.OutputTokens = wire.Usage.Output
	}

	for _, raw := range wire.Output {
		var item struct {
			Type string `json:"type"`
			Call
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}

		if json.Unmarshal(raw, &item) != nil {
			return Round{}, errors.New("分析模型输出格式异常")
		}

		switch item.Type {
		case "web_search_call", "file_search_call", "computer_call":
			return Round{}, ErrNativeTool
		case "function_call":
			if item.ID == "" || len(item.ID) > 255 || len(item.Arguments) > 32000 || len(item.Name) > 128 || item.Name == "" {
				return Round{}, errors.New("模型工具请求无效")
			}
			out.Calls = append(out.Calls, item.Call)
		case "message":
			if item.Role != "assistant" {
				continue
			}

			for _, content := range item.Content {
				if content.Type == "refusal" {
					return Round{}, errors.New("模型未能执行该分析，请调整需求")
				}

				if content.Type == "output_text" {
					out.Text += content.Text
				}
			}
		}
	}

	return out, nil
}
