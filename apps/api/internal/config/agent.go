package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

type Agent struct {
	Mode     string
	MCPURL   string
	MCPToken string
}

func LoadAgent() (Agent, error) {
	cfg := Agent{Mode: strings.TrimSpace(os.Getenv("AGENT_MODE")), MCPURL: strings.TrimSpace(os.Getenv("AGENT_MCP_URL")), MCPToken: os.Getenv("AGENT_MCP_TOKEN")}
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

	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "host.docker.internal" || u.Hostname() == "query-mcp"
	if u.Scheme != "https" && !(os.Getenv("APP_ENV") != "production" && local && u.Scheme == "http") {
		return Agent{}, errors.New("MCP 连接必须使用 HTTPS，本地明确允许的地址除外")
	}

	return cfg, nil
}
