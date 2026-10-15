package config

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type LLM struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

func LoadLLM() (LLM, error) {
	return loadLLM(os.Getenv)
}

func loadLLM(get func(string) string) (LLM, error) {
	cfg := LLM{
		APIKey:  strings.TrimSpace(get("OPENAI_API_KEY")),
		BaseURL: strings.TrimRight(strings.TrimSpace(get("OPENAI_BASE_URL")), "/"),
		Model:   strings.TrimSpace(get("OPENAI_MODEL")),
		Timeout: 120 * time.Second,
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}

	if cfg.APIKey == "" && cfg.Model == "" {
		return cfg, nil
	}

	if cfg.APIKey == "" || cfg.Model == "" || len(cfg.Model) > 128 || strings.ContainsAny(cfg.APIKey, "\r\n") {
		return LLM{}, errors.New("请同时配置有效的 OPENAI_API_KEY 和 OPENAI_MODEL")
	}

	endpoint, err := url.Parse(cfg.BaseURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return LLM{}, errors.New("OPENAI_BASE_URL 格式无效")
	}

	local := endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "localhost" || endpoint.Hostname() == "::1"
	if endpoint.Scheme != "https" && !(get("APP_ENV") != "production" && local && endpoint.Scheme == "http") {
		return LLM{}, errors.New("OPENAI_BASE_URL 必须使用 HTTPS（本地测试回环地址除外）")
	}

	if raw := get("OPENAI_TIMEOUT_SECONDS"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 10 || seconds > 300 {
			return LLM{}, errors.New("OPENAI_TIMEOUT_SECONDS 必须为 10–300 的整数")
		}

		cfg.Timeout = time.Duration(seconds) * time.Second
	}

	return cfg, nil
}
