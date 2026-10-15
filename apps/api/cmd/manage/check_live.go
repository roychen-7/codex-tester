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
	"github.com/roychen-7/codex-tester/apps/api/internal/querymcp"
)

// Explicit manual smoke only: one-hour usage aggregate, no business detail or
// application conversation persistence. Never invoked by make check or CI.
func checkLive() error {
	cfg, err := config.LoadLLM()
	if err != nil {
		return err
	}
	remote, err := config.LoadAgent()
	if err != nil {
		return err
	}
	if cfg.APIKey == "" || remote.DataSource != "live" || remote.MCPURL == "" {
		return errors.New("check-live 需要已配置模型和 live MCP")
	}
	end := time.Now().UTC().Truncate(time.Hour)
	start := end.Add(-time.Hour)
	question := fmt.Sprintf("请查询线上 usage_logs，在 UTC %s（含）至 %s（不含）内的日志记录条数。只执行一条聚合 SELECT count(*) AS log_count FROM analytics.usage_logs，不代表成功请求，不查询其他指标或客户明细。直接给出带查询依据的结果。", start.Format(time.RFC3339), end.Format(time.RFC3339))
	snapshot, _ := json.Marshal(map[string]any{"current_time": time.Now().UTC().Format(time.RFC3339), "summary": "", "messages": []map[string]string{{"id": "1", "role": "user", "content": question}}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	model := &agent.Conversation{Model: llm.DialogueRounder{Model: llm.NewOpenAI(cfg)}, Config: remote}
	expectedSQL, _ := querymcp.Validate("SELECT count(*) AS log_count FROM analytics.usage_logs", nil)
	executions := 0
	response, err := model.ConverseRecorded(ctx, snapshot, "check-live:"+fmt.Sprint(time.Now().UnixNano()), func(_ context.Context, e agent.Event) error {
		if e.Status == "running" {
			if e.Kind == "tool" && e.Name == "prepare_query" {
				var in querymcp.PrepareInput
				if json.Unmarshal(e.Input, &in) != nil {
					return errors.New("联调查询参数无效")
				}
				actual, err := querymcp.Validate(in.SQL, in.Parameters)
				a, ae := time.Parse(time.RFC3339, in.WindowStart)
				b, be := time.Parse(time.RFC3339, in.WindowEnd)
				if err != nil || actual != expectedSQL || len(in.Parameters) != 0 || ae != nil || be != nil || !a.Equal(start) || !b.Equal(end) {
					return errors.New("联调只允许指定一小时日志条数聚合")
				}
			}
			if e.Kind == "tool" && e.Name == "execute_query" {
				executions++
				if executions > 1 {
					return errors.New("联调最多执行一次查询")
				}
			}
			fmt.Printf("步骤 %d：%s\n", e.Sequence, e.Name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	var evidence []agent.Evidence
	if json.Unmarshal(response.Evidence, &evidence) != nil || len(evidence) != 1 || response.Result.WaitingForUser || len(response.Result.ResultIDs) != 1 {
		return errors.New("线上联调没有完成单次带依据的回答")
	}
	e := evidence[0]
	a, errA := time.Parse(time.RFC3339, e.WindowStart)
	b, errB := time.Parse(time.RFC3339, e.WindowEnd)
	if errA != nil || errB != nil || !a.Equal(start) || !b.Equal(end) || e.Result.DataSource != "live" || len(e.Result.Rows) != 1 || e.Result.Truncated {
		return errors.New("线上联调结果来源、范围或行数不符合预期")
	}
	fmt.Printf("线上只读联调通过：%s 至 %s，返回1行聚合，未保存会话。\n", e.WindowStart, e.WindowEnd)
	fmt.Println(response.Result.Reply)
	return nil
}
