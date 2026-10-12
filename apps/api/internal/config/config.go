package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
)

type Config struct {
	Environment string
	Host        string
	Port        int
	LogLevel    slog.Level
}

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Config, error) {
	value := func(key, fallback string) string {
		if v, ok := lookup(key); ok {
			return v
		}
		return fallback
	}
	cfg := Config{Environment: value("APP_ENV", "local"), Host: value("HOST", "127.0.0.1")}
	if cfg.Environment != "local" && cfg.Environment != "production" {
		return Config{}, fmt.Errorf("APP_ENV 必须是 local 或 production")
	}
	if net.ParseIP(cfg.Host) == nil {
		return Config{}, fmt.Errorf("HOST 必须是有效的 IPv4 或 IPv6 地址")
	}
	port, err := strconv.Atoi(value("PORT", "3000"))
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("PORT 必须是 1–65535 的整数")
	}
	cfg.Port = port
	level := value("LOG_LEVEL", "")
	// An omitted or empty level selects the environment's default.
	if level == "" {
		level = "info"
		if cfg.Environment == "local" {
			level = "debug"
		}
	}
	levels := map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError}
	var ok bool
	cfg.LogLevel, ok = levels[level]
	if !ok {
		return Config{}, fmt.Errorf("LOG_LEVEL 必须是 debug、info、warn 或 error")
	}
	return cfg, nil
}
