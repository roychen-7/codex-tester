package llm

import (
	"errors"
	"strings"
)

// Conclusion links a user-facing finding to evidence, not private reasoning.
type Conclusion struct {
	Text        string   `json:"text"`
	Kind        string   `json:"kind"`
	SourceIDs   []string `json:"source_ids"`
	ResultIDs   []string `json:"result_ids"`
	Rationale   string   `json:"rationale"`
	Limitations string   `json:"limitations"`
}

func ValidateConclusions(items []Conclusion) error {
	if len(items) > 8 {
		return errors.New("结论数量超过上限")
	}

	for _, item := range items {
		if strings.TrimSpace(item.Text) == "" || len(item.Text) > 2000 || strings.TrimSpace(item.Rationale) == "" || len(item.Rationale) > 2000 || len(item.Limitations) > 2000 {
			return errors.New("结论内容或依据说明无效")
		}

		if item.Kind != "fact" && item.Kind != "inference" && item.Kind != "recommendation" {
			return errors.New("结论类型无效")
		}

		if item.Kind != "fact" && strings.TrimSpace(item.Limitations) == "" {
			return errors.New("推断与建议必须说明限制或待验证项")
		}

		if len(item.SourceIDs)+len(item.ResultIDs) == 0 || len(item.SourceIDs)+len(item.ResultIDs) > 8 {
			return errors.New("结论须关联1至8条依据")
		}

		for _, ids := range [][]string{item.SourceIDs, item.ResultIDs} {
			seen := map[string]bool{}
			for _, id := range ids {
				if strings.TrimSpace(id) == "" || len(id) > 128 || seen[id] {
					return errors.New("结论引用无效或重复")
				}

				seen[id] = true
			}
		}
	}

	return nil
}

func conclusionSchema() map[string]any {
	text := map[string]string{"type": "string"}
	ids := map[string]any{"type": "array", "items": text}
	return map[string]any{"type": "array", "items": map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"text": text, "kind": map[string]any{"type": "string", "enum": []string{"fact", "inference", "recommendation"}}, "source_ids": ids, "result_ids": ids, "rationale": text, "limitations": text},
		"required":   []string{"text", "kind", "source_ids", "result_ids", "rationale", "limitations"},
	}}
}
