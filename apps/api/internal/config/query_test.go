package config

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestQueryConfiguration(t *testing.T) {
	clearQueryPostgres(t)
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

func clearQueryPostgres(t *testing.T) {
	t.Helper()
	for _, key := range []string{"DSN", "HOST", "PORT", "DATABASE", "USER", "PASSWORD", "SSLMODE"} {
		t.Setenv("QUERY_POSTGRES_"+key, "")
	}
}

func TestQuerySplitConfiguration(t *testing.T) {
	for _, host := range []string{"localhost", "2001:db8::1"} {
		t.Run(host, func(t *testing.T) {
			clearQueryPostgres(t)
			t.Setenv("APP_ENV", "production")
			t.Setenv("QUERY_MCP_TOKEN", strings.Repeat("t", 32))
			t.Setenv("QUERY_POSTGRES_HOST", host)
			t.Setenv("QUERY_POSTGRES_DATABASE", "db/name?#%")
			t.Setenv("QUERY_POSTGRES_USER", "reader@name")
			password := "p@ss:#%/?&= 空格'\\$"
			t.Setenv("QUERY_POSTGRES_PASSWORD", password)
			cfg, err := LoadQueryMCP()
			if err != nil {
				t.Fatal(err)
			}

			parsed, err := pgconn.ParseConfig(cfg.DSN)
			if err != nil {
				t.Fatal("generated connection URL rejected")
			}
			if parsed.Host != host || parsed.Port != 5432 || parsed.Database != "db/name?#%" || parsed.User != "reader@name" || parsed.Password != password || parsed.TLSConfig == nil {
				t.Fatal("connection fields did not round trip")
			}
		})
	}
}

func TestQuerySplitConfigurationRejectsInvalidInput(t *testing.T) {
	cases := []struct{ name, key, value string }{
		{"mixed", "DSN", "postgres://reader:secret@localhost/test"},
		{"missing host", "HOST", ""},
		{"missing database", "DATABASE", ""},
		{"missing user", "USER", ""},
		{"missing password", "PASSWORD", ""},
		{"port zero", "PORT", "0"},
		{"port overflow", "PORT", "65536"},
		{"port invalid", "PORT", "secret"},
		{"ssl invalid", "SSLMODE", "secret"},
		{"ssl insecure", "SSLMODE", "disable"},
		{"host URL", "HOST", "https://secret"},
		{"host port", "HOST", "localhost:5432"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			clearQueryPostgres(t)
			t.Setenv("APP_ENV", "production")
			t.Setenv("QUERY_MCP_TOKEN", strings.Repeat("t", 32))
			t.Setenv("QUERY_POSTGRES_HOST", "localhost")
			t.Setenv("QUERY_POSTGRES_DATABASE", "test")
			t.Setenv("QUERY_POSTGRES_USER", "reader")
			t.Setenv("QUERY_POSTGRES_PASSWORD", "secret")
			t.Setenv("QUERY_POSTGRES_"+tt.key, tt.value)
			_, err := LoadQueryMCP()
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("invalid configuration accepted or secret leaked")
			}
		})
	}
}

func TestDatabaseInspectionConfigurationWithoutMCP(t *testing.T) {
	clearQueryPostgres(t)
	t.Setenv("APP_ENV", "local")
	t.Setenv("QUERY_MCP_TOKEN", "")
	t.Setenv("QUERY_POSTGRES_DSN", "postgres://reader:secret@localhost/test?sslmode=disable")
	if _, err := LoadQueryPostgresDSN(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQueryMCP(); err == nil {
		t.Fatal("MCP must still require its own token")
	}
}

func TestQueryDataSourceMode(t *testing.T) {
	clearQueryPostgres(t)
	t.Setenv("APP_ENV", "local")
	t.Setenv("QUERY_POSTGRES_DSN", "postgres://reader:secret@localhost/test?sslmode=disable")
	t.Setenv("QUERY_MCP_TOKEN", strings.Repeat("t", 32))
	for _, source := range []string{"synthetic", "live", "invalid"} {
		t.Setenv("QUERY_DATA_SOURCE", source)
		cfg, err := LoadQueryMCP()
		if source == "invalid" {
			if err == nil {
				t.Fatal("unknown source accepted")
			}
		} else if err != nil || cfg.Source != source {
			t.Fatal("source lost", err)
		}
	}
}
