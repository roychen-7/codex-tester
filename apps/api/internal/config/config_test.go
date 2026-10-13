package config

import (
	"log/slog"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  map[string]string
		want Config
	}{
		{"local defaults", nil, Config{"local", "127.0.0.1", 3000, slog.LevelDebug}},
		{"production defaults", map[string]string{"APP_ENV": "production"}, Config{"production", "127.0.0.1", 3000, slog.LevelInfo}},
		{"container override", map[string]string{"HOST": "0.0.0.0", "PORT": "13001", "LOG_LEVEL": "warn"}, Config{"local", "0.0.0.0", 13001, slog.LevelWarn}},
		{"IPv6 and maximum port", map[string]string{"HOST": "::1", "PORT": "65535", "LOG_LEVEL": "error"}, Config{"local", "::1", 65535, slog.LevelError}},
		{"production empty level", map[string]string{"APP_ENV": "production", "LOG_LEVEL": ""}, Config{"production", "127.0.0.1", 3000, slog.LevelInfo}},
		{"explicit info", map[string]string{"LOG_LEVEL": "info", "PORT": "1"}, Config{"local", "127.0.0.1", 1, slog.LevelInfo}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := load(func(key string) (string, bool) {
				v, ok := tt.env[key]
				return v, ok
			})
			if err != nil || got != tt.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for key, values := range map[string][]string{
		"APP_ENV":   {"", "prod", "staging"},
		"HOST":      {"", "http://localhost", "127.0.0.999"},
		"PORT":      {"", "0", "65536", "-1", "abc", "3.5"},
		"LOG_LEVEL": {"verbose", "INFO", "secret-value"},
	} {
		for _, value := range values {
			t.Run(key+"/"+value, func(t *testing.T) {
				_, err := load(func(k string) (string, bool) {
					if k == key {
						return value, true
					}

					if k == "APP_ENV" {
						return "production", true
					}

					return "", false
				})
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("expected %s error, got %v", key, err)
				}

				if strings.Contains(err.Error(), "secret-value") {
					t.Fatal("error leaked input")
				}
			})
		}
	}
}
