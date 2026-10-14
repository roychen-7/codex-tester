package config

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

type QueryMCP struct {
	DSN     string
	Token   string
	Address string
}

func LoadQueryMCP() (QueryMCP, error) {
	cfg := QueryMCP{DSN: os.Getenv("QUERY_POSTGRES_DSN"), Token: os.Getenv("QUERY_MCP_TOKEN"), Address: os.Getenv("QUERY_MCP_ADDRESS")}
	if cfg.Address == "" {
		cfg.Address = "127.0.0.1:13003"
	}

	u, err := url.Parse(cfg.DSN)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.User == nil {
		return QueryMCP{}, errors.New("QUERY_POSTGRES_DSN 必须是 PostgreSQL 连接 URL")
	}

	if len(cfg.Token) < 32 || strings.ContainsAny(cfg.Token, "\r\n") {
		return QueryMCP{}, errors.New("QUERY_MCP_TOKEN 至少 32 字节且不能包含换行")
	}

	if os.Getenv("APP_ENV") == "production" && u.Query().Get("sslmode") != "verify-full" {
		return QueryMCP{}, errors.New("生产查询连接必须使用 sslmode=verify-full")
	}

	return cfg, nil
}

func TestPostgresDSN() string {
	return os.Getenv("TEST_POSTGRES_DSN")
}
