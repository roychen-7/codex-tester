package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
)

type researchHistory struct{}

func (researchHistory) Search(context.Context, agent.HistorySearch) (agent.HistoryMatches, error) {
	return agent.HistoryMatches{Items: []agent.Source{researchSource()}, Coverage: "仅为联调构造的历史记录"}, nil
}

func (researchHistory) Read(_ context.Context, id string) ([]agent.Source, error) {
	if id != "1" {
		return nil, errors.New("测试消息不存在")
	}

	return []agent.Source{researchSource()}, nil
}

func researchSource() agent.Source {
	return agent.Source{ID: "history:1", Kind: "history", Title: "联调测试目标", ConversationID: "1", MessageID: "1", Role: "user", Content: "这是联调构造的记录：先验证公开网页搜索与正文阅读，暂不连接生产业务数据库。", CreatedAt: "2026-10-01T00:00:00Z", RetrievedAt: time.Now().UTC().Format(time.RFC3339)}
}

// Uses synthetic history and public web information; never reads or writes MySQL.
func checkResearch() error {
	cfg, err := config.LoadLLM()
	if err != nil {
		return err
	}

	remote, err := config.LoadAgent()
	if err != nil {
		return err
	}

	if cfg.APIKey == "" || remote.TavilyAPIKey == "" {
		return errors.New("请先配置模型与 TAVILY_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	model := &agent.Conversation{Model: researchRounder{model: llm.DialogueRounder{Model: llm.NewOpenAI(cfg)}}, Config: remote, History: researchHistory{}}
	question := "这是能力联调。先检索历史里我们约定的验证目标，再搜索并阅读 Tavily 官方 Search 或 Extract 文档。用简短中文说明这两个能力分别做什么，以及它们是否符合历史目标；引用至少一条历史原文和一条已阅读网页。只搜索一次、读一个官网页面即可，无需查数据库。"
	if len(os.Args) > 2 && os.Args[2] == "comparison" {
		question = "这是公开资料研究能力测试，不涉及真实客户。请先检索历史中的验证目标，再对比 OpenRouter 与 SiliconFlow 官网公开的定价模式和功能优势，给出两条适用于通用 API 服务的新用户留存建议。说明事实与假设，引用历史原文和已阅读的官方网页。最多3次搜索、5页正文，不查询业务数据库。"
	}
	snapshot, _ := json.Marshal(map[string]any{"current_time": time.Now().UTC().Format(time.RFC3339), "summary": "", "messages": []map[string]string{{"id": "2", "role": "user", "content": question}}})
	used := map[string]bool{}
	response, err := model.ConverseRecorded(ctx, snapshot, "research-smoke", func(_ context.Context, event agent.Event) error {
		if event.Kind == "tool" && event.Status != "running" {
			fmt.Printf("tool=%s status=%s\n", event.Name, event.Status)
			if event.Status == "failed" {
				fmt.Printf("tool_error=%s\n", event.Output)
			}
			if event.Status == "succeeded" {
				used[event.Name] = true
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	var sources []agent.Source
	if err := json.Unmarshal(response.Sources, &sources); err != nil {
		return err
	}

	kinds := map[string]bool{}
	for _, source := range sources {
		kinds[source.Kind] = true
	}

	fmt.Println(response.Result.Reply)
	fmt.Printf("conclusions=%d\n", len(response.Result.Conclusions))
	if len(response.Result.Conclusions) == 0 {
		return errors.New("分析未返回结论与依据对应关系")
	}
	if !used["search_history"] || !used["read_history"] || !used["search_public_web"] || !used["read_webpage"] || !kinds["history"] || !kinds["web"] {
		return errors.New("未完成历史与网页组合引用")
	}

	fmt.Println("历史+联网组合验证通过；历史为构造数据，未读取业务库或写入会话。")
	return nil
}

// Log only tool names and wire item types for this synthetic smoke test.
type researchRounder struct{ model llm.Rounder }

func (r researchRounder) Next(ctx context.Context, instructions string, input []json.RawMessage, tools []llm.Tool) (llm.Round, error) {
	names := []string{}
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	fmt.Printf("available_tools=%v\n", names)
	out, err := r.model.Next(ctx, instructions, input, tools)
	if err != nil {
		return out, err
	}
	types := []string{}
	for _, item := range out.Items {
		var value struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(item, &value) == nil {
			types = append(types, value.Type)
		}
	}
	fmt.Printf("response_item_types=%v\n", types)
	return out, nil
}
