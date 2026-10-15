package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
)

// checkAgent is deliberately restricted to the local synthetic MCP fixture.
func checkAgent() error {
	cfg, err := config.LoadLLM()
	if err != nil {
		return err
	}

	if cfg.APIKey == "" {
		return errors.New("请先配置模型")
	}

	remoteCfg, err := config.LoadAgent()
	if err != nil {
		return err
	}

	endpoint, err := url.Parse(remoteCfg.MCPURL)
	if err != nil || endpoint == nil || (endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "host.docker.internal") || endpoint.Port() != "13003" || remoteCfg.MCPToken != "query-test-only-token-not-for-production" {
		return errors.New("check-agent 仅允许连接本地模拟 MCP 13003")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}

	if remoteCfg.Mode == "conversation" {
		return checkConversationTools(ctx, cfg, remoteCfg, fmt.Sprintf("check:%x", nonce))
	}

	remote, err := agent.Connect(ctx, remoteCfg, fmt.Sprintf("check:%x", nonce))
	if err != nil {
		return err
	}
	defer remote.Close()
	requirement := json.RawMessage(`{"goal":"模拟数据：统计2026年9月每日充值金额和订单数","details":[{"name":"完整口径","value":"用户已明确确认：UTC，2026-09-01 00:00:00（含）至2026-10-01 00:00:00（不含）；全部客户，payment_orders 中 status=COMPLETED 且 order_type=balance，按 paid_at 的 UTC 日期分组；金额 SUM(pay_amount)，订单数 COUNT(*)，不扣退款；直接给出表格与合计。仅为模拟数据。"}]}`)
	out, err := agent.Run(ctx, llm.NewOpenAI(cfg), remote, requirement, func(_ context.Context, e agent.Event) error {
		if e.Status == "running" {
			fmt.Printf("步骤 %d：%s\n", e.Sequence, e.Name)
		}

		if e.Status == "failed" {
			fmt.Printf("步骤 %d 被工具拒绝\n", e.Sequence)
		}

		return nil
	})
	if err != nil {
		return err
	}

	if len(out.Answer.Questions) > 0 || len(out.Evidence) == 0 {
		return errors.New("模型仍需澄清，完整查询联调尚未通过")
	}
	fmt.Printf("Agent 多轮工具调用通过，共 %d 个查询结果；未写入会话。\n", len(out.Evidence))
	fmt.Println(out.Answer.Reply)
	return nil
}

// checkConversationTools exercises the production conversation adapter without
// creating users, conversations or messages in the application database.
func checkConversationTools(ctx context.Context, cfg config.LLM, remoteCfg config.Agent, subject string) error {
	model := &agent.Conversation{Model: llm.DialogueRounder{Model: llm.NewOpenAI(cfg)}, Config: remoteCfg}
	messages := []map[string]string{{"id": "1", "role": "user", "content": "请查询模拟数据：UTC，2026-09-01（含）到2026-10-01（不含），所有客户在 payment_orders 中 status=COMPLETED 且 order_type=balance 的充值。按 paid_at 过滤，不扣退款。只查一行 SUM(pay_amount) AS total、COUNT(*) AS orders，给出总金额和订单数。口径已明确，直接查询即可。"}}
	snapshot := map[string]any{"current_time": time.Now().UTC().Format(time.RFC3339), "summary": "", "messages": messages}
	var prior []agent.Evidence
	for turn := 0; turn < 2; turn++ {
		data, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		queries, reads := 0, 0
		reply, err := model.ConverseRecorded(ctx, data, subject, func(_ context.Context, e agent.Event) error {
			if e.Status == "running" {
				fmt.Printf("第 %d 轮，步骤 %d：%s\n", turn+1, e.Sequence, e.Name)
				if e.Name == "execute_query" {
					queries++
				}
				if e.Name == "read_result" {
					reads++
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if reply.Result.WaitingForUser || len(reply.Result.ResultIDs) == 0 {
			return errors.New("完整口径未完成带依据的回答")
		}
		var evidence []agent.Evidence
		if json.Unmarshal(reply.Evidence, &evidence) != nil || len(evidence) == 0 {
			return errors.New("未保存查询依据")
		}
		fmt.Println(reply.Result.Reply)
		if turn == 0 {
			verified := false
			for _, e := range evidence {
				if len(e.Result.Rows) != 1 {
					continue
				}
				values := map[string]string{}
				for i, column := range e.Result.Columns {
					values[column] = fmt.Sprint(e.Result.Rows[0][i])
				}
				if (values["total"] == "170.00" || values["total"] == "170") && values["orders"] == "3" {
					verified = true
				}
			}
			if !verified || queries == 0 {
				return errors.New("模拟查询结果与170/3不符")
			}
			prior = evidence
			messages = append(messages, map[string]string{"id": "2", "role": "assistant", "content": reply.Result.Reply}, map[string]string{"id": "3", "role": "user", "content": "沿用刚才已经查到的数据，不重新查询数据库，计算平均每笔充值金额。"})
			snapshot["messages"] = messages
			snapshot["summary"] = reply.Result.ContextSummary
			snapshot["evidence"] = evidence
		} else {
			if queries != 0 || reads == 0 || evidence[0].Result.ResultID != prior[0].Result.ResultID {
				return errors.New("历史查询续聊未通过")
			}
		}
	}
	fmt.Println("连续对话查询与历史依据续聊通过；仅使用模拟数据，未写入会话。")
	return nil
}
