package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
)

type Tools interface {
	Definitions() []llm.Tool
	Call(context.Context, string, json.RawMessage) (json.RawMessage, error)
	Close() error
}

type MCP struct {
	session *mcp.ClientSession
	tools   []llm.Tool
}

type authTransport struct {
	token   string
	subject string
}

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	clone.Header.Set("X-Agent-Subject", t.subject)
	return http.DefaultTransport.RoundTrip(clone)
}

func Connect(ctx context.Context, cfg config.Agent, subject string) (Tools, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "ddshub-agent", Version: "0.1.0"}, nil)
	httpClient := &http.Client{Timeout: 20 * time.Second, Transport: authTransport{token: cfg.MCPToken, subject: subject}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("MCP 不允许重定向") }}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: cfg.MCPURL, HTTPClient: httpClient}, nil)
	if err != nil {
		return nil, errors.New("无法连接数据查询 MCP，请检查服务配置")
	}

	list, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		session.Close()
		return nil, errors.New("无法读取 MCP 工具定义")
	}

	out := &MCP{session: session}
	seen := map[string]bool{}
	for _, tool := range list.Tools {
		if tool.Name != "describe_dataset" && tool.Name != "prepare_query" && tool.Name != "execute_query" {
			continue
		}

		if seen[tool.Name] {
			session.Close()
			return nil, errors.New("MCP 工具定义重复")
		}
		seen[tool.Name] = true
		out.tools = append(out.tools, llm.Tool{Type: "function", Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema})
	}

	if len(out.tools) != 3 {
		session.Close()
		return nil, errors.New("MCP 缺少必需的查询工具")
	}

	return out, nil
}

func (m *MCP) Definitions() []llm.Tool {
	return m.tools
}

func (m *MCP) Call(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	response, err := m.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, errors.New("MCP 请求失败或超时")
	}

	if response.IsError {
		// This endpoint is configured by the operator; forward bounded tool errors, never transport errors/credentials.
		for _, item := range response.Content {
			if text, ok := item.(*mcp.TextContent); ok && len(text.Text) <= 1000 {
				return nil, errors.New(text.Text)
			}
		}

		return nil, errors.New("查询工具拒绝了请求，请检查 SQL 或参数")
	}

	raw, err := json.Marshal(response.StructuredContent)
	if err != nil || len(raw) > 1024*1024 || string(raw) == "null" {
		return nil, errors.New("MCP 返回结构无效")
	}

	return raw, nil
}

func (m *MCP) Close() error {
	return m.session.Close()
}
