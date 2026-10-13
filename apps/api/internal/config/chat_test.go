package config

import (
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestChatConfiguration(t *testing.T) {
	values := map[string]string{"MYSQL_PASSWORD": "not-logged", "MYSQL_HOST": "mysql"}
	lookup := func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}

	cfg, err := loadChat(lookup)
	if err != nil {
		t.Fatal(err)
	}

	dsn, err := mysql.ParseDSN(cfg.DSN)
	if err != nil ||
		dsn.Addr != "mysql:13306" ||
		dsn.DBName != "agent" ||
		cfg.SessionTTL != 7*24*time.Hour ||
		cfg.SecureCookies {
		t.Fatal("unexpected database/session configuration")
	}

	for _, tc := range []struct{ key, value string }{
		{"MYSQL_PASSWORD", ""},
		{"MYSQL_PORT", "0"},
		{"MYSQL_HOST", ""},
		{"SESSION_TTL_HOURS", "0"},
		{"SESSION_TTL_HOURS", "721"},
		{"MYSQL_TLS", "skip-verify"},
		{"COOKIE_SECURE", "invalid"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			_, err := loadChat(func(key string) (string, bool) {
				if key == tc.key {
					return tc.value, true
				}

				return lookup(key)
			})
			if err == nil || strings.Contains(err.Error(), "not-logged") {
				t.Fatal("invalid configuration accepted or secret leaked")
			}
		})
	}

	values["APP_ENV"] = "production"
	values["COOKIE_SECURE"] = "false"
	if _, err := loadChat(lookup); err == nil {
		t.Fatal("insecure production cookie accepted")
	}
}
