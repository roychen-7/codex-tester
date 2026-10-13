package config

import (
	"strings"
	"testing"
	"time"
)

func TestLLMConfiguration(t *testing.T) {
	values := map[string]string{}
	get := func(key string) string {
		return values[key]
	}

	cfg, err := loadLLM(get)
	if err != nil || cfg.APIKey != "" || cfg.Timeout != 120*time.Second {
		t.Fatal("session-only configuration failed")
	}

	values["OPENAI_API_KEY"] = "test-secret-never-log"
	if _, err := loadLLM(get); err == nil || strings.Contains(err.Error(), values["OPENAI_API_KEY"]) {
		t.Fatal("missing model accepted or key leaked")
	}

	values["OPENAI_MODEL"] = "test-model"
	values["OPENAI_BASE_URL"] = "http://127.0.0.1:13002/v1"
	if _, err := loadLLM(get); err != nil {
		t.Fatal(err)
	}

	for _, endpoint := range []string{"http://example.com/v1", "https://user:secret@example.com", "https://example.com/v1?key=secret", "file:///tmp/key"} {
		values["OPENAI_BASE_URL"] = endpoint
		if _, err := loadLLM(get); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe endpoint accepted or leaked")
		}
	}

	values["OPENAI_BASE_URL"] = "https://api.openai.com/v1"
	for _, timeout := range []string{"0", "301", "invalid"} {
		values["OPENAI_TIMEOUT_SECONDS"] = timeout
		if _, err := loadLLM(get); err == nil {
			t.Fatal("invalid timeout accepted")
		}
	}
}
