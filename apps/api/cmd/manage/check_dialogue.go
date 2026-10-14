package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
)

// checkDialogue uses invented conversation content and does not write to MySQL.
func checkDialogue() error {
	cfg, err := config.LoadLLM()
	if err != nil {
		return err
	}
	if cfg.APIKey == "" {
		return errors.New("请先配置模型")
	}

	model := &agent.Conversation{Model: llm.DialogueRounder{Model: llm.NewOpenAI(cfg)}}
	messages := []map[string]string{{"id": "1", "role": "user", "content": "我想优化网站。你先问我一个最关键的问题，帮我确定方向。"}}
	summary := ""
	for round := 0; round < 2; round++ {
		snapshot, err := json.Marshal(map[string]any{"current_time": time.Now().UTC().Format(time.RFC3339), "summary": summary, "messages": messages, "history_truncated": false})
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		reply, err := model.Converse(ctx, snapshot)
		cancel()
		if err != nil {
			return err
		}

		fmt.Printf("第 %d 轮，等待补充=%t：\n%s\n", round+1, reply.Result.WaitingForUser, reply.Result.Reply)
		if round == 0 {
			if !reply.Result.WaitingForUser {
				return errors.New("模型未标记等待用户补充")
			}
			summary = reply.Result.ContextSummary
			messages = append(messages, map[string]string{"id": "2", "role": "assistant", "content": reply.Result.Reply}, map[string]string{"id": "3", "role": "user", "content": "这是一个 API 服务网站，我最关心新用户留存。现在只讨论通用方法，请给我两条建议，不需要更多信息，也不要假装查询过数据。"})
		} else if reply.Result.WaitingForUser {
			return errors.New("模型仍在追问，直接回答场景未通过")
		}
	}

	fmt.Println("连续对话、追问与补充后回答校验通过；未查询业务数据或写入会话。")
	return nil
}
