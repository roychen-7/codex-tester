package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/roychen-7/codex-tester/apps/api/internal/chat"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/database"
)

const cookieName = "agent_session"

type attempt struct {
	Count int
	Until time.Time
}

type limiter struct {
	sync.Mutex
	Entries map[string]attempt
}

func (l *limiter) allow(ip string) bool {
	l.Lock()
	defer l.Unlock()

	now := time.Now()
	for key, v := range l.Entries {
		if !v.Until.After(now) {
			delete(l.Entries, key)
		}
	}

	a, ok := l.Entries[ip]
	if !ok {
		if len(l.Entries) >= 10000 {
			return false
		}

		a = attempt{Until: now.Add(15 * time.Minute)}
	}

	if a.Count >= 20 {
		return false
	}

	a.Count++
	l.Entries[ip] = a
	return true
}

func NewChat(store *chat.Store, cfg config.Chat) *gin.Engine {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.RedirectTrailingSlash = false
	r.RedirectFixedPath = false

	r.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")

		defer func() {
			if recover() != nil {
				slog.Error("chat request panic")
				fail(c, 500, "INTERNAL_ERROR", "服务器内部错误。")
			}
		}()

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" && c.Request.Method != "OPTIONS" {
			// A custom header requires a browser CORS preflight. This service grants no cross-origin CORS access.
			if c.GetHeader("X-Agent-Request") != "1" || c.GetHeader("Sec-Fetch-Site") == "cross-site" {
				fail(c, 403, "CSRF_REJECTED", "请求来源校验失败。")
				return
			}
		}

		c.Next()
	})

	r.NoRoute(func(c *gin.Context) {
		fail(c, 404, "NOT_FOUND", "接口不存在。")
	})

	r.GET("/health", func(c *gin.Context) {
		if err := database.Ready(c.Request.Context(), store.DB); err != nil {
			fail(c, 503, "DATABASE_UNAVAILABLE", "数据库未就绪。")
			return
		}

		c.JSON(200, gin.H{"data": gin.H{"status": "ok"}})
	})

	limit := &limiter{Entries: map[string]attempt{}}
	setCookie := func(c *gin.Context, token string, maxAge int) {
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     cookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   cfg.SecureCookies,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   maxAge,
		})
	}

	r.POST("/auth/login", func(c *gin.Context) {
		if !limit.allow(c.ClientIP()) {
			c.Header("Retry-After", "900")
			fail(c, 429, "TOO_MANY_ATTEMPTS", "登录尝试过多，请稍后再试。")
			return
		}

		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeChat(c, &input) {
			return
		}

		if len(input.Username) > 64 || len(input.Password) > 72 || input.Username == "" || input.Password == "" {
			chatError(c, chat.ErrAuth)
			return
		}

		u, token, err := store.Login(c.Request.Context(), input.Username, input.Password, cfg.SessionTTL)
		if err != nil {
			chatError(c, err)
			return
		}

		// Rotate an existing browser session without affecting other browsers.
		if old, _ := c.Cookie(cookieName); old != "" {
			if err = store.Logout(c.Request.Context(), old); err != nil {
				chatError(c, err)
				return
			}
		}

		setCookie(c, token, int(cfg.SessionTTL.Seconds()))
		c.JSON(200, gin.H{"data": u})
	})

	r.POST("/auth/logout", func(c *gin.Context) {
		token, _ := c.Cookie(cookieName)
		if token != "" {
			if err := store.Logout(c.Request.Context(), token); err != nil {
				chatError(c, err)
				return
			}
		}

		setCookie(c, "", -1)
		c.Status(204)
	})

	protected := r.Group("")
	protected.Use(func(c *gin.Context) {
		token, _ := c.Cookie(cookieName)
		u, err := store.Authenticate(c.Request.Context(), token)
		if err != nil {
			chatError(c, err)
			return
		}

		c.Set("user", u)
		c.Next()
	})

	user := func(c *gin.Context) uint64 {
		return c.MustGet("user").(chat.User).ID
	}

	protected.GET("/auth/me", func(c *gin.Context) {
		c.JSON(200, gin.H{"data": c.MustGet("user")})
	})

	protected.GET("/conversations", func(c *gin.Context) {
		n, ok := pageLimit(c)
		if !ok {
			return
		}

		if len(c.Query("cursor")) > 512 {
			chatError(c, chat.ErrInput)
			return
		}

		p, err := store.Conversations(c.Request.Context(), user(c), c.Query("cursor"), n)
		if err != nil {
			chatError(c, err)
			return
		}

		c.JSON(200, gin.H{"data": p})
	})

	protected.POST("/conversations", func(c *gin.Context) {
		var input struct {
			Title string `json:"title"`
		}
		if !decodeChat(c, &input) {
			return
		}

		if input.Title == "" {
			input.Title = "新会话"
		}

		v, err := store.CreateConversation(c.Request.Context(), user(c), input.Title)
		if err != nil {
			chatError(c, err)
			return
		}

		c.JSON(201, gin.H{"data": v})
	})

	convo := protected.Group("/conversations/:id")
	convo.Use(func(c *gin.Context) {
		if !chat.ValidID(c.Param("id")) {
			chatError(c, chat.ErrNotFound)
			return
		}

		c.Next()
	})

	convo.GET("", func(c *gin.Context) {
		v, err := store.Conversation(c.Request.Context(), user(c), c.Param("id"))
		if err != nil {
			chatError(c, err)
			return
		}

		c.JSON(200, gin.H{"data": v})
	})

	convo.PATCH("", func(c *gin.Context) {
		var input struct {
			Title string `json:"title"`
		}
		if !decodeChat(c, &input) {
			return
		}

		v, err := store.Rename(c.Request.Context(), user(c), c.Param("id"), input.Title)
		if err != nil {
			chatError(c, err)
			return
		}

		c.JSON(200, gin.H{"data": v})
	})

	convo.GET("/messages", func(c *gin.Context) {
		n, ok := pageLimit(c)
		if !ok {
			return
		}

		p, err := store.Messages(c.Request.Context(), user(c), c.Param("id"), c.Query("before"), n)
		if err != nil {
			chatError(c, err)
			return
		}

		c.JSON(200, gin.H{"data": p})
	})

	convo.POST("/messages", func(c *gin.Context) {
		var input struct {
			RequestID string `json:"request_id"`
			Content   string `json:"content"`
		}
		if !decodeChat(c, &input) {
			return
		}

		v, created, err := store.Send(c.Request.Context(), user(c), c.Param("id"), input.RequestID, input.Content)
		if err != nil {
			chatError(c, err)
			return
		}

		code := 200
		if created {
			code = 201
		}

		c.JSON(code, gin.H{"data": v})
	})

	agentRoutes(convo, store)

	return r
}

func pageLimit(c *gin.Context) (int, bool) {
	n := 20
	var err error
	if raw, ok := c.GetQuery("limit"); ok {
		n, err = strconv.Atoi(raw)
	}

	if err != nil || n < 1 || n > 100 {
		chatError(c, chat.ErrInput)
		return 0, false
	}

	return n, true
}

func decodeChat(c *gin.Context, dst any) bool {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || strings.ToLower(media) != "application/json" {
		fail(c, 415, "UNSUPPORTED_MEDIA_TYPE", "请使用 application/json。")
		return false
	}

	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(c, 413, "BODY_TOO_LARGE", "请求体不能超过 64 KiB。")
		} else {
			fail(c, 400, "INVALID_JSON", "无法读取请求。")
		}

		return false
	}

	if !utf8.Valid(raw) || len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw))[0] != '{' {
		fail(c, 400, "INVALID_JSON", "请求必须为 JSON 对象。")
		return false
	}

	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil {
		fail(c, 400, "INVALID_JSON", "请求字段或格式不正确。")
		return false
	}

	var extra any
	if decoder.Decode(&extra) != io.EOF {
		fail(c, 400, "INVALID_JSON", "只能包含一个 JSON 对象。")
		return false
	}

	return true
}

func chatError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, chat.ErrBusy), errors.Is(err, chat.ErrTaskChanged):
		fail(c, 409, "STATE_CONFLICT", err.Error())
	case errors.Is(err, chat.ErrLLMDisabled):
		fail(c, 503, "LLM_DISABLED", err.Error())
	case errors.Is(err, chat.ErrInput):
		fail(c, 400, "INVALID_INPUT", err.Error())
	case errors.Is(err, chat.ErrAuth):
		fail(c, 401, "INVALID_CREDENTIALS", err.Error())
	case errors.Is(err, chat.ErrSession):
		fail(c, 401, "UNAUTHENTICATED", err.Error())
	case errors.Is(err, chat.ErrNotFound):
		fail(c, 404, "NOT_FOUND", err.Error())
	case errors.Is(err, chat.ErrConflict):
		fail(c, 409, "IDEMPOTENCY_CONFLICT", err.Error())
	default:
		slog.Error("chat storage operation failed")
		fail(c, 500, "INTERNAL_ERROR", "暂时无法完成操作，请重试。")
	}
}
