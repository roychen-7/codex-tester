package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Chat struct {
	DSN           string
	SessionTTL    time.Duration
	SecureCookies bool
}

// TestDSN is only used by integration tests; production configuration never reads it.
func TestDSN() string {
	return os.Getenv("TEST_MYSQL_DSN")
}

func LoadChat() (Chat, error) {
	return loadChat(os.LookupEnv)
}

func loadChat(lookup func(string) (string, bool)) (Chat, error) {
	value := func(key, fallback string) string {
		if v, ok := lookup(key); ok {
			return v
		}

		return fallback
	}

	host := value("MYSQL_HOST", "127.0.0.1")
	port := value("MYSQL_PORT", "13306")
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return Chat{}, fmt.Errorf("MYSQL_PORT 必须是有效端口")
	}

	name := value("MYSQL_DATABASE", "agent")
	user := value("MYSQL_USER", "agent")
	password := value("MYSQL_PASSWORD", "")
	if strings.TrimSpace(host) == "" || name == "" || user == "" || password == "" {
		return Chat{}, fmt.Errorf("MYSQL_HOST、MYSQL_DATABASE、MYSQL_USER、MYSQL_PASSWORD 不得为空")
	}

	hours, err := strconv.Atoi(value("SESSION_TTL_HOURS", "168"))
	if err != nil || hours < 1 || hours > 720 {
		return Chat{}, fmt.Errorf("SESSION_TTL_HOURS 必须是 1–720 的整数")
	}

	production := value("APP_ENV", "local") == "production"
	secure, err := strconv.ParseBool(value("COOKIE_SECURE", strconv.FormatBool(production)))
	if err != nil || production && !secure {
		return Chat{}, fmt.Errorf("COOKIE_SECURE 必须为布尔值，production 必须为 true")
	}

	tls := value("MYSQL_TLS", "false")
	if tls != "false" && tls != "true" {
		return Chat{}, fmt.Errorf("MYSQL_TLS 必须为 true 或 false")
	}

	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(host, port)
	cfg.DBName = name
	cfg.User = user
	cfg.Passwd = password
	cfg.TLSConfig = tls

	return Chat{
		DSN:           cfg.FormatDSN(),
		SessionTTL:    time.Duration(hours) * time.Hour,
		SecureCookies: secure,
	}, nil
}
