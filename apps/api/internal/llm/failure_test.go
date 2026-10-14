package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/config"
)

func TestFailureDetailsRedactsUnknownErrors(t *testing.T) {
	code, message := FailureDetails(fmt.Errorf("wrapped: %w", requestError("model_timeout", "timeout")))
	if code != "model_timeout" || message != "timeout" {
		t.Fatal("typed failure lost")
	}
	code, message = FailureDetails(errors.New("secret-token secret-url secret-body"))
	if code != "generation_failed" || strings.Contains(message, "secret") {
		t.Fatal("unknown failure leaked")
	}
}

func TestRequestFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{
		{401, "model_auth"},
		{403, "model_auth"},
		{429, "model_rate_limit"},
		{503, "model_unavailable"},
		{400, "model_request_rejected"},
	} {
		t.Run(tc.code+fmt.Sprint(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("secret-provider-body"))
			}))
			defer server.Close()
			client := NewOpenAI(config.LLM{BaseURL: server.URL, APIKey: "secret-key", Timeout: time.Second})
			_, err := client.Converse(context.Background(), json.RawMessage(`{}`))
			code, message := FailureDetails(err)
			if code != tc.code || strings.Contains(message, "secret") || strings.Contains(message, server.URL) {
				t.Fatal("incorrect or unsafe failure", code, message)
			}
		})
	}

	for _, headersFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("timeout_headers_%t", headersFirst), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if headersFirst {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				select {
				case <-r.Context().Done():
				case <-time.After(150 * time.Millisecond):
				}
			}))
			defer server.Close()
			client := NewOpenAI(config.LLM{BaseURL: server.URL, Timeout: 30 * time.Millisecond})
			_, err := client.Converse(context.Background(), json.RawMessage(`{}`))
			code, message := FailureDetails(err)
			if code != "model_timeout" || !strings.Contains(message, "超时") {
				t.Fatal("timeout misclassified", code, err)
			}
		})
	}
}
