package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type QueryMCP struct {
	Source  string
	DSN     string
	Token   string
	Address string
}

func LoadQueryMCP() (QueryMCP, error) {
	dsn, err := LoadQueryPostgresDSN()
	if err != nil {
		return QueryMCP{}, err
	}

	cfg := QueryMCP{Source: os.Getenv("QUERY_DATA_SOURCE"), DSN: dsn, Token: os.Getenv("QUERY_MCP_TOKEN"), Address: os.Getenv("QUERY_MCP_ADDRESS")}
	if cfg.Source == "" {
		cfg.Source = "synthetic"
	}
	if cfg.Source != "synthetic" && cfg.Source != "live" {
		return QueryMCP{}, errors.New("QUERY_DATA_SOURCE 仅允许 synthetic/live")
	}
	if cfg.Address == "" {
		cfg.Address = "127.0.0.1:13003"
	}

	if len(cfg.Token) < 32 || strings.ContainsAny(cfg.Token, "\r\n") {
		return QueryMCP{}, errors.New("QUERY_MCP_TOKEN 至少 32 字节且不能包含换行")
	}

	return cfg, nil
}

// LoadQueryPostgresDSN validates database-only configuration without requiring MCP credentials.
func LoadQueryPostgresDSN() (string, error) {
	dsn, err := queryPostgresDSN()
	if err != nil {
		return "", err
	}

	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.User == nil {
		return "", errors.New("PostgreSQL 连接配置无效，请检查分项配置或 QUERY_POSTGRES_DSN")
	}
	if os.Getenv("APP_ENV") == "production" && u.Query().Get("sslmode") != "verify-full" {
		return "", errors.New("生产查询连接必须使用 sslmode=verify-full")
	}

	return dsn, nil
}

func TestPostgresDSN() string {
	return os.Getenv("TEST_POSTGRES_DSN")
}

// queryPostgresDSN encodes credentials as URL components, never as SQL.
func queryPostgresDSN() (string, error) {
	keys := []string{"HOST", "PORT", "DATABASE", "USER", "PASSWORD", "SSLMODE"}
	split := false
	for _, key := range keys {
		split = split || os.Getenv("QUERY_POSTGRES_"+key) != ""
	}

	dsn := os.Getenv("QUERY_POSTGRES_DSN")
	if !split {
		return dsn, nil
	}
	if dsn != "" {
		return "", errors.New("QUERY_POSTGRES_DSN 与分项连接配置不能同时使用")
	}

	host := os.Getenv("QUERY_POSTGRES_HOST")
	database := os.Getenv("QUERY_POSTGRES_DATABASE")
	user := os.Getenv("QUERY_POSTGRES_USER")
	password := os.Getenv("QUERY_POSTGRES_PASSWORD")
	if host == "" || database == "" || user == "" || password == "" {
		return "", errors.New("PostgreSQL 分项配置需要 HOST、DATABASE、USER 和 PASSWORD")
	}
	if strings.ContainsAny(host, "/@?# \t\r\n") {
		return "", errors.New("QUERY_POSTGRES_HOST 必须是主机名或 IP 地址，不含协议和端口")
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return "", errors.New("QUERY_POSTGRES_HOST 中 IPv6 使用不带方括号的地址，端口请单独配置")
	}

	port := os.Getenv("QUERY_POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", errors.New("QUERY_POSTGRES_PORT 必须在 1–65535 之间")
	}

	sslmode := os.Getenv("QUERY_POSTGRES_SSLMODE")
	if sslmode == "" {
		sslmode = "verify-full"
	}
	switch sslmode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return "", errors.New("QUERY_POSTGRES_SSLMODE 无效")
	}

	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     net.JoinHostPort(host, strconv.Itoa(number)),
		Path:     "/" + database,
		RawQuery: url.Values{"sslmode": {sslmode}}.Encode(),
	}
	return u.String(), nil
}
