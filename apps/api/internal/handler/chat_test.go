package handler_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/roychen-7/codex-tester/apps/api/internal/chat"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/database"
	"github.com/roychen-7/codex-tester/apps/api/internal/handler"
	"gorm.io/gorm"
)

func chatRequest(router http.Handler, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Request", "1")
	if cookie != nil {
		req.AddCookie(cookie)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decoded[T any](t *testing.T, response *httptest.ResponseRecorder, status int) T {
	t.Helper()

	if response.Code != status {
		t.Fatalf("want %d, got %d: %s", status, response.Code, response.Body.String())
	}

	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}

	return envelope.Data
}

func TestChatMySQLIntegration(t *testing.T) {
	dsn := config.TestDSN()
	if dsn == "" {
		t.Skip("设置 TEST_MYSQL_DSN 启用真实 MySQL 集成测试")
	}

	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(parsed.DBName, "_test") {
		t.Fatal("集成测试仅允许使用 _test 结尾的独立数据库")
	}

	ctx := context.Background()
	db, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	pool, _ := db.DB()
	defer pool.Close()
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal("migration must be repeatable", err)
	}

	store := &chat.Store{DB: db}
	seed := make([]byte, 6)
	if _, err := rand.Read(seed); err != nil {
		t.Fatal(err)
	}

	prefix := "test_" + hex.EncodeToString(seed)
	password := "test-password-1234"
	var users []uint64
	defer func() {
		// Delete only rows owned by the users created in this test, never truncate a database.
		if len(users) == 0 {
			return
		}

		var conversations []chat.Conversation
		db.Where("user_id IN ?", users).Find(&conversations)
		for _, conversation := range conversations {
			db.Where("conversation_id = ?", conversation.ID).Delete(&chat.Message{})
		}

		db.Where("user_id IN ?", users).Delete(&chat.Conversation{})
		db.Where("user_id IN ?", users).Delete(&chat.Session{})
		db.Where("id IN ?", users).Delete(&chat.Account{})
	}()

	for _, suffix := range []string{"_a", "_b"} {
		u, err := store.CreateUser(ctx, prefix+suffix, "测试用户", password)
		if err != nil {
			t.Fatal(err)
		}

		users = append(users, u.ID)
	}

	if _, err := store.CreateUser(ctx, prefix+"_a", "重复", password); err != chat.ErrUserExists {
		t.Fatalf("duplicate user: %v", err)
	}

	cfg := config.Chat{SessionTTL: 7 * 24 * time.Hour, SecureCookies: true}
	router := handler.NewChat(store, cfg)
	login := func(username string) *http.Cookie {
		t.Helper()

		body, _ := json.Marshal(map[string]string{"username": username, "password": password})
		response := chatRequest(router, nil, "POST", "/auth/login", string(body))
		decoded[chat.User](t, response, 200)
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatal("unsafe session cookie")
		}

		return cookies[0]
	}

	a, b := login(prefix+"_a"), login(prefix+"_b")
	for _, tc := range []struct {
		cookie *http.Cookie
		path   string
		code   int
	}{
		{nil, "/conversations", 401},
		{a, "/auth/me", 200},
	} {
		if w := chatRequest(router, tc.cookie, "GET", tc.path, ""); w.Code != tc.code {
			t.Fatal(w.Code)
		}
	}

	wrong, _ := json.Marshal(map[string]string{"username": prefix + "_a", "password": "wrong"})
	if w := chatRequest(router, nil, "POST", "/auth/login", string(wrong)); w.Code != 401 {
		t.Fatal("wrong password accepted")
	}

	conversation := decoded[chat.Conversation](t, chatRequest(router, a, "POST", "/conversations", `{}`), 201)
	path := "/conversations/" + strconv.FormatUint(conversation.ID, 10)
	for _, tc := range []struct{ method, suffix, body string }{
		{"GET", "", ""},
		{"GET", "/messages", ""},
		{"GET", "/agent", ""},
		{"PATCH", "", `{"title":"stolen"}`},
		{"POST", "/messages", `{"request_id":"11111111-1111-4111-8111-111111111111","content":"stolen"}`},
	} {
		if w := chatRequest(router, b, tc.method, path+tc.suffix, tc.body); w.Code != 404 {
			t.Fatalf("cross-user access: %d %s", w.Code, w.Body.String())
		}
	}

	agentRouter := handler.NewChat(&chat.Store{DB: db, LLMModel: "test-model"}, cfg)
	for _, tc := range []struct {
		cookie *http.Cookie
		method string
		suffix string
		body   string
		code   int
	}{
		{nil, "GET", "/agent", "", 401},
		{nil, "POST", "/tasks/1/confirm", `{"revision":1}`, 401},
		{nil, "POST", "/runs/1/retry", `{}`, 401},
		{b, "POST", "/tasks/1/confirm", `{"revision":1}`, 404},
		{b, "POST", "/runs/1/retry", `{}`, 404},
		{a, "POST", "/tasks/1/confirm", `{}`, 400},
		{a, "POST", "/tasks/1/confirm", `{"revision":-1}`, 400},
		{a, "POST", "/tasks/1/confirm", `{"revision":1,"status":"confirmed"}`, 400},
		{a, "POST", "/runs/1/retry", `null`, 400},
		{a, "POST", "/runs/1/retry", `{"model":"override"}`, 400},
	} {
		w := chatRequest(agentRouter, tc.cookie, tc.method, path+tc.suffix, tc.body)
		if w.Code != tc.code {
			t.Fatalf("agent route %s: want %d, got %d", tc.suffix, tc.code, w.Code)
		}
	}

	otherPage := decoded[chat.Page[chat.Conversation]](t, chatRequest(router, b, "GET", "/conversations", ""), 200)
	if len(otherPage.Items) != 0 {
		t.Fatal("other user saw conversation")
	}

	messageBody := `{"request_id":"11111111-1111-4111-8111-111111111111","content":"分析本月使用情况 👋"}`
	message := decoded[chat.Message](t, chatRequest(router, a, "POST", path+"/messages", messageBody), 201)
	beforeRetry, _ := store.Conversation(ctx, users[0], strconv.FormatUint(conversation.ID, 10))
	retry := decoded[chat.Message](t, chatRequest(router, a, "POST", path+"/messages", messageBody), 200)
	afterRetry, _ := store.Conversation(ctx, users[0], strconv.FormatUint(conversation.ID, 10))
	if retry.ID != message.ID || !beforeRetry.UpdatedAt.Equal(afterRetry.UpdatedAt) {
		t.Fatal("retry was not idempotent")
	}

	conflict := strings.Replace(messageBody, "分析本月使用情况 👋", "changed", 1)
	if w := chatRequest(router, a, "POST", path+"/messages", conflict); w.Code != 409 {
		t.Fatal("idempotency conflict accepted")
	}

	renamed := decoded[chat.Conversation](t, chatRequest(router, a, "PATCH", path, `{"title":"  月度分析  "}`), 200)
	if renamed.Title != "月度分析" {
		t.Fatal("rename failed")
	}

	for _, body := range []string{
		`null`,
		`[]`,
		`{`,
		`{} {}`,
		`{"request_id":"bad","content":"hello"}`,
		`{"request_id":"11111111-1111-4111-8111-111111111111","content":" "}`,
		`{"role":"assistant","content":"fake"}`,
	} {
		if w := chatRequest(router, a, "POST", path+"/messages", body); w.Code != 400 {
			t.Fatalf("invalid body accepted: %d", w.Code)
		}
	}

	if w := chatRequest(router, a, "POST", path+"/messages", strings.Repeat(" ", 65537)); w.Code != 413 {
		t.Fatal("oversized body accepted")
	}

	if w := chatRequest(router, a, "GET", "/conversations?cursor=broken", ""); w.Code != 400 {
		t.Fatal("invalid cursor accepted")
	}

	if w := chatRequest(router, a, "GET", path+"/messages?limit=101", ""); w.Code != 400 {
		t.Fatal("invalid limit accepted")
	}

	csrf := httptest.NewRequest("POST", "/auth/logout", strings.NewReader(`{}`))
	csrf.AddCookie(a)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, csrf)
	if w.Code != 403 {
		t.Fatal("missing CSRF header accepted")
	}

	csrf.Header.Set("X-Agent-Request", "1")
	csrf.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, csrf)
	if w.Code != 403 {
		t.Fatal("cross-site write accepted")
	}

	// Concurrent retries serialize on the conversation and create one message.
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			_, _, err := store.Send(ctx, users[0], strconv.FormatUint(conversation.ID, 10), "22222222-2222-4222-8222-222222222222", "并发消息")
			results <- err
		}()
	}

	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}

	// Failure after INSERT must roll back the message, not leave a partial write.
	const callback = "test:fail_conversation_update"
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		tx.AddError(fmt.Errorf("injected update failure"))
	}); err != nil {
		t.Fatal(err)
	}

	_, _, rollbackErr := store.Send(ctx, users[0], strconv.FormatUint(conversation.ID, 10), "33333333-3333-4333-8333-333333333333", "应回滚")
	db.Callback().Update().Remove(callback)
	if rollbackErr == nil {
		t.Fatal("injected failure ignored")
	}

	var count int64
	db.Model(&chat.Message{}).Where("conversation_id = ?", conversation.ID).Count(&count)
	if count != 2 {
		t.Fatalf("unexpected message count %d", count)
	}

	// Reopen the database and router: this verifies persistence beyond in-memory state.
	db2, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	pool2, _ := db2.DB()
	defer pool2.Close()

	router = handler.NewChat(&chat.Store{DB: db2}, cfg)
	page := decoded[chat.Page[chat.Message]](t, chatRequest(router, a, "GET", path+"/messages?limit=1", ""), 200)
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal("message cursor missing")
	}

	older := decoded[chat.Page[chat.Message]](t, chatRequest(router, a, "GET", path+"/messages?limit=1&before="+page.NextCursor, ""), 200)
	if len(older.Items) != 1 || older.Items[0].ID != message.ID || older.NextCursor != "" {
		t.Fatal("history pagination failed")
	}

	decoded[chat.Conversation](t, chatRequest(router, a, "POST", "/conversations", `{"title":"第二段对话"}`), 201)
	list := decoded[chat.Page[chat.Conversation]](t, chatRequest(router, a, "GET", "/conversations?limit=1", ""), 200)
	list2 := decoded[chat.Page[chat.Conversation]](t, chatRequest(router, a, "GET", "/conversations?limit=1&cursor="+list.NextCursor, ""), 200)
	if len(list.Items) != 1 || len(list2.Items) != 1 || list.Items[0].ID == list2.Items[0].ID {
		t.Fatal("conversation pagination failed")
	}

	if w := chatRequest(router, a, "POST", "/auth/logout", `{}`); w.Code != 204 {
		t.Fatal("logout failed")
	}

	if w := chatRequest(router, a, "GET", "/auth/me", ""); w.Code != 401 {
		t.Fatal("revoked token accepted")
	}

	a = login(prefix + "_a")
	if w := chatRequest(router, a, "GET", path, ""); w.Code != 200 {
		t.Fatal("history lost on re-login")
	}

	// Expiry must remain after creation to satisfy the database constraint.
	db.Model(&chat.Session{}).
		Where("user_id = ?", users[0]).
		Updates(map[string]any{
			"created_at": time.Now().UTC().Add(-2 * time.Hour),
			"expires_at": time.Now().UTC().Add(-time.Hour),
		})
	if w := chatRequest(router, a, "GET", "/auth/me", ""); w.Code != 401 {
		t.Fatal("expired token accepted")
	}

	db.Model(&chat.Account{}).Where("id = ?", users[1]).Update("status", "disabled")
	if w := chatRequest(router, b, "GET", "/auth/me", ""); w.Code != 401 {
		t.Fatal("disabled user accepted")
	}
}
