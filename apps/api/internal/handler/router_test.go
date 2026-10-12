package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/roychen-7/codex-tester/apps/api/internal/handler"
	"github.com/roychen-7/codex-tester/apps/api/internal/task"
)

func request(router http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestTaskLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := handler.New(task.NewStore())
	send := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := request(router, method, path, body, "application/json")
		if r.Code != status {
			t.Fatalf("%s %s: got %d: %s", method, path, r.Code, r.Body.String())
		}
		return r
	}
	if r := send("GET", "/health", "", 200); r.Body.String() != `{"data":{"status":"ok"}}` {
		t.Fatal(r.Body.String())
	}
	if r := send("GET", "/tasks", "", 200); r.Body.String() != `{"data":[]}` {
		t.Fatal(r.Body.String())
	}
	created := send("POST", "/tasks", `{"title":"  接口测试  "}`, 201)
	var envelope struct {
		Data task.Task `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	item := envelope.Data
	if item.Title != "接口测试" || item.Status != "todo" || len(item.ID) != 36 {
		t.Fatalf("unexpected task: %+v", item)
	}
	path := "/tasks/" + item.ID
	for _, status := range []string{"doing", "done", "todo"} {
		r := send("PATCH", path, `{"status":"`+status+`"}`, 200)
		if err := json.Unmarshal(r.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data.Status != status || envelope.Data.ID != item.ID {
			t.Fatal(envelope.Data)
		}
	}
	r := send("GET", "/tasks", "", 200)
	var list struct {
		Data []task.Task `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 1 || list.Data[0] != item {
		t.Fatal(list.Data)
	}
	if r := send("DELETE", path, "", 204); r.Body.Len() != 0 {
		t.Fatal("204 must have no body")
	}
	if r := send("GET", "/tasks", "", 200); r.Body.String() != `{"data":[]}` {
		t.Fatal(r.Body.String())
	}
	send("DELETE", path, "", 404)
	send("PATCH", path, `{"status":"done"}`, 404)
}

func TestInputErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name, method, path, body, media string
		status                          int
		code                            string
	}{
		{"missing title", "POST", "/tasks", `{}`, "application/json", 400, "INVALID_TITLE"},
		{"blank title", "POST", "/tasks", `{"title":"  "}`, "application/json", 400, "INVALID_TITLE"},
		{"numeric title", "POST", "/tasks", `{"title":42}`, "application/json", 400, "INVALID_TITLE"},
		{"long title", "POST", "/tasks", `{"title":"` + strings.Repeat("x", 121) + `"}`, "application/json", 400, "INVALID_TITLE"},
		{"array", "POST", "/tasks", `[]`, "application/json", 400, "INVALID_BODY"},
		{"null", "POST", "/tasks", `null`, "application/json", 400, "INVALID_BODY"},
		{"primitive", "POST", "/tasks", `true`, "application/json", 400, "INVALID_BODY"},
		{"malformed", "POST", "/tasks", `{`, "application/json", 400, "INVALID_JSON"},
		{"empty", "POST", "/tasks", ``, "application/json", 400, "INVALID_JSON"},
		{"multiple values", "POST", "/tasks", `{} {}`, "application/json", 400, "INVALID_JSON"},
		{"media type", "POST", "/tasks", `{}`, "text/plain", 415, "UNSUPPORTED_MEDIA_TYPE"},
		{"large body", "POST", "/tasks", `{"title":"` + strings.Repeat("x", 16384) + `"}`, "application/json", 413, "BODY_TOO_LARGE"},
		{"invalid status", "PATCH", "/tasks/missing", `{"status":"invalid"}`, "application/json", 400, "INVALID_STATUS"},
		{"missing status", "PATCH", "/tasks/missing", `{}`, "application/json", 400, "INVALID_STATUS"},
		{"missing resource", "PATCH", "/tasks/missing", `{"status":"done"}`, "application/json", 404, "NOT_FOUND"},
		{"missing route", "GET", "/missing", ``, "", 404, "NOT_FOUND"},
		{"trailing slash", "GET", "/tasks/", ``, "", 404, "NOT_FOUND"},
		{"unsupported method", "PUT", "/tasks", ``, "", 404, "NOT_FOUND"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := handler.New(task.NewStore())
			r := request(router, tc.method, tc.path, tc.body, tc.media)
			if r.Code != tc.status {
				t.Fatalf("got %d: %s", r.Code, r.Body.String())
			}
			var result struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Error.Code != tc.code || result.Error.Message == "" {
				t.Fatal(r.Body.String())
			}
			if r := request(router, "GET", "/tasks", "", ""); r.Body.String() != `{"data":[]}` {
				t.Fatal("invalid request changed data")
			}
		})
	}
}

func TestBodyLimitAndCharset(t *testing.T) {
	router := handler.New(task.NewStore())
	body := `{"title":"边界"}`
	body += strings.Repeat(" ", 16*1024-len(body))
	r := request(router, "POST", "/tasks", body, "Application/JSON; charset=utf-8")
	if r.Code != 201 {
		t.Fatalf("exactly 16 KiB should succeed: %d %s", r.Code, r.Body.String())
	}
}
