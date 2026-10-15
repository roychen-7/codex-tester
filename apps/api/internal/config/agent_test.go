package config

import (
	"strings"
	"testing"
)

func TestAgentConfig(t *testing.T) {
	for _, tc := range []struct {
		name, url, token, env string
		valid                 bool
	}{
		{"disabled", "", "", "local", true},
		{"local", "http://127.0.0.1:13003/mcp", "12345678901234567890123456789012", "local", true},
		{"production TLS", "http://127.0.0.1:13003/mcp", "12345678901234567890123456789012", "production", false},
		{"partial", "https://example.com/mcp", "", "local", false},
		{"credentials", "https://secret@example.com/mcp", "12345678901234567890123456789012", "local", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENT_MCP_URL", tc.url)
			t.Setenv("AGENT_MCP_TOKEN", tc.token)
			t.Setenv("APP_ENV", tc.env)
			_, err := LoadAgent()
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected config result %v", err)
			}
		})
	}
}

func TestAgentMode(t *testing.T) {
	t.Setenv("AGENT_MCP_URL", "")
	t.Setenv("AGENT_MCP_TOKEN", "")
	for _, mode := range []string{"", "conversation", "legacy", "invalid"} {
		t.Setenv("AGENT_MODE", mode)
		cfg, err := LoadAgent()
		if mode == "invalid" {
			if err == nil {
				t.Fatal("invalid mode accepted")
			}
			continue
		}
		if err != nil || (mode == "" && cfg.Mode != "conversation") || (mode != "" && cfg.Mode != mode) {
			t.Fatal("mode configuration invalid", cfg, err)
		}
	}
}

func TestTavilyConfig(t *testing.T) {
	t.Setenv("AGENT_MCP_URL", "")
	t.Setenv("AGENT_MCP_TOKEN", "")
	t.Setenv("AGENT_MODE", "conversation")
	for _, tc := range []struct {
		key, endpoint, environment string
		valid                      bool
	}{
		{"", "", "local", true},
		{"test-key", "https://api.tavily.com", "production", true},
		{"bad\nkey", "", "local", false},
		{"test", "http://127.0.0.1:13002", "local", true},
		{"test", "http://127.0.0.1:13002", "production", false},
		{"test", "https://untrusted.example", "local", false},
	} {
		t.Setenv("TAVILY_API_KEY", tc.key)
		t.Setenv("TAVILY_BASE_URL", tc.endpoint)
		t.Setenv("APP_ENV", tc.environment)
		cfg, err := LoadAgent()
		if (err == nil) != tc.valid {
			t.Fatal("unexpected config outcome", err)
		}
		if err == nil && cfg.TavilyBaseURL == "" {
			t.Fatal("missing default endpoint")
		}
	}
}

func TestLiveAgentConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("TAVILY_BASE_URL", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Setenv("AGENT_MCP_URL", "http://query-live:3001/mcp")
	t.Setenv("AGENT_MCP_TOKEN", strings.Repeat("t", 32))
	t.Setenv("AGENT_MODE", "conversation")
	t.Setenv("AGENT_DATA_SOURCE", "live")
	cfg, err := LoadAgent()
	if err != nil || cfg.DataSource != "live" {
		t.Fatal("live configuration rejected", err)
	}
	t.Setenv("AGENT_DATA_SOURCE", "other")
	if _, err := LoadAgent(); err == nil {
		t.Fatal("unknown source accepted")
	}
	t.Setenv("AGENT_DATA_SOURCE", "live")
	t.Setenv("AGENT_MODE", "legacy")
	if _, err := LoadAgent(); err == nil {
		t.Fatal("legacy live mode accepted")
	}
}
