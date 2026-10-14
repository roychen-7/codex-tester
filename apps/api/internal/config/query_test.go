package config

import (
	"strings"
	"testing"
)

func TestQueryConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("QUERY_POSTGRES_DSN", "postgres://analyst:secret-value@localhost/sub2api_test?sslmode=disable")
	t.Setenv("QUERY_MCP_TOKEN", strings.Repeat("t", 32))
	t.Setenv("QUERY_MCP_ADDRESS", "")
	cfg, err := LoadQueryMCP()
	if err != nil || cfg.Address != "127.0.0.1:13003" {
		t.Fatal("valid config rejected", err)
	}

	t.Setenv("APP_ENV", "production")
	if _, err := LoadQueryMCP(); err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatal("insecure production config or secret leaked")
	}

	t.Setenv("QUERY_POSTGRES_DSN", "postgres://analyst:secret-value@example.invalid/sub2api?sslmode=verify-full")
	if _, err := LoadQueryMCP(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("QUERY_MCP_TOKEN", "short")
	if _, err := LoadQueryMCP(); err == nil {
		t.Fatal("weak token accepted")
	}
}
