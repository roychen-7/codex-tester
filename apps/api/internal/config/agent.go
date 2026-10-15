package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

type Agent struct {
	DataSource    string
	TavilyBaseURL string
	TavilyAPIKey  string
	Mode          string
	MCPURL        string
	MCPToken      string
}

func LoadAgent() (Agent, error) {
	cfg := Agent{
		DataSource:    os.Getenv("AGENT_DATA_SOURCE"),
		TavilyBaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("TAVILY_BASE_URL")), "/"),
		TavilyAPIKey:  strings.TrimSpace(os.Getenv("TAVILY_API_KEY")),
		Mode:          strings.TrimSpace(os.Getenv("AGENT_MODE")),
		MCPURL:        strings.TrimSpace(os.Getenv("AGENT_MCP_URL")),
		MCPToken:      os.Getenv("AGENT_MCP_TOKEN"),
	}
	if cfg.DataSource == "" {
		cfg.DataSource = "synthetic"
	}
	if cfg.DataSource != "synthetic" && cfg.DataSource != "live" {
		return Agent{}, errors.New("AGENT_DATA_SOURCE 仅允许 synthetic/live")
	}
	if cfg.DataSource == "live" && cfg.Mode == "legacy" {
		return Agent{}, errors.New("线上查询仅支持 conversation 模式")
	}
	if strings.ContainsAny(cfg.TavilyAPIKey, "\r\n") || len(cfg.TavilyAPIKey) > 512 {
		return Agent{}, errors.New("TAVILY_API_KEY invalid")
	}
	if cfg.TavilyBaseURL == "" {
		cfg.TavilyBaseURL = "https://api.tavily.com"
	}
	if cfg.TavilyBaseURL != "https://api.tavily.com" {
		u, err := url.Parse(cfg.TavilyBaseURL)
		if err != nil || os.Getenv("APP_ENV") == "production" || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return Agent{}, errors.New("TAVILY_BASE_URL 仅允许官方地址，开发测试可使用本机回环 HTTP")
		}
	}

	if cfg.Mode == "" {
		cfg.Mode = "conversation"
	}
	if cfg.Mode != "conversation" && cfg.Mode != "legacy" {
		return Agent{}, errors.New("AGENT_MODE must be conversation or legacy")
	}

	if cfg.MCPURL == "" && cfg.MCPToken == "" {
		return cfg, nil
	}

	u, err := url.Parse(cfg.MCPURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/mcp" || len(cfg.MCPToken) < 32 || strings.ContainsAny(cfg.MCPToken, "\r\n") {
		return Agent{}, errors.New("请配置有效的 AGENT_MCP_URL 和 AGENT_MCP_TOKEN")
	}

	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "host.docker.internal" || u.Hostname() == "query-mcp" || u.Hostname() == "query-live"
	if u.Scheme != "https" && !(os.Getenv("APP_ENV") != "production" && local && u.Scheme == "http") {
		return Agent{}, errors.New("MCP 连接必须使用 HTTPS，本地明确允许的地址除外")
	}

	return cfg, nil
}
