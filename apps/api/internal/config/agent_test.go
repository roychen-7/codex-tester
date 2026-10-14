package config

import "testing"

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
