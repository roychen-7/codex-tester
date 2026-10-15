package llm

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const PromptVersion = "clarification-v1"

type Detail struct {
	Name             string   `json:"name"`
	Value            string   `json:"value"`
	Source           string   `json:"source"`
	SourceMessageIDs []string `json:"source_message_ids"`
}

type Requirement struct {
	Goal    string   `json:"goal"`
	Details []Detail `json:"details"`
}

type Result struct {
	Reply                string      `json:"reply"`
	Requirement          Requirement `json:"requirement"`
	MissingInformation   []string    `json:"missing_information"`
	ReadyForConfirmation bool        `json:"ready_for_confirmation"`
	ContextSummary       string      `json:"context_summary"`
}

type Response struct {
	Result       Result
	ProviderID   string
	InputTokens  *uint64
	OutputTokens *uint64
}

func (r Result) Validate() error {
	if strings.TrimSpace(r.Reply) == "" || len(r.Reply) > 40000 ||
		len(r.Requirement.Goal) > 4000 || len(r.Requirement.Details) > 40 ||
		r.Requirement.Details == nil || r.MissingInformation == nil || len(r.MissingInformation) > 3 ||
		strings.TrimSpace(r.ContextSummary) == "" || len(r.ContextSummary) > 12000 {
		return errors.New("模型返回内容不符合要求，请重试")
	}

	if r.ReadyForConfirmation && (strings.TrimSpace(r.Requirement.Goal) == "" || len(r.MissingInformation) != 0) {
		return errors.New("模型返回的需求状态不一致，请重试")
	}

	for _, question := range r.MissingInformation {
		if strings.TrimSpace(question) == "" || len(question) > 2000 {
			return errors.New("模型返回的问题不符合要求，请重试")
		}
	}

	for _, detail := range r.Requirement.Details {
		if strings.TrimSpace(detail.Name) == "" || len(detail.Name) > 200 ||
			strings.TrimSpace(detail.Value) == "" || len(detail.Value) > 4000 ||
			detail.SourceMessageIDs == nil || len(detail.SourceMessageIDs) > 20 ||
			(detail.Source != "user" && detail.Source != "history" && detail.Source != "suggestion") ||
			(detail.Source == "user" && len(detail.SourceMessageIDs) == 0) {
			return errors.New("模型返回的需求来源不符合要求，请重试")
		}
	}

	return nil
}

func DecodeResult(data []byte) (Result, error) {
	var result Result
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return result, errors.New("模型返回格式异常，请重试")
	}

	for _, name := range []string{"reply", "requirement", "missing_information", "ready_for_confirmation", "context_summary"} {
		if len(fields[name]) == 0 || string(fields[name]) == "null" {
			return result, errors.New("模型返回字段缺失，请重试")
		}
	}

	var requirement map[string]json.RawMessage
	if json.Unmarshal(fields["requirement"], &requirement) != nil || len(requirement["goal"]) == 0 || string(requirement["goal"]) == "null" {
		return result, errors.New("模型返回需求格式异常，请重试")
	}

	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, errors.New("模型返回格式异常，请重试")
	}

	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return result, errors.New("模型返回格式异常，请重试")
	}

	return result, result.Validate()
}
