package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/roychen-7/codex-tester/apps/api/internal/task"
)

func fail(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

func readBody(c *gin.Context) (map[string]any, bool) {
	mediaType := strings.TrimSpace(strings.SplitN(c.GetHeader("Content-Type"), ";", 2)[0])
	if !strings.EqualFold(mediaType, "application/json") {
		fail(c, 415, "UNSUPPORTED_MEDIA_TYPE", "请使用 application/json。")
		return nil, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(c, 413, "BODY_TOO_LARGE", "请求体不能超过 16 KiB。")
		} else {
			fail(c, 400, "INVALID_JSON", "请求体必须是有效 JSON。")
		}
		return nil, false
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		fail(c, 400, "INVALID_JSON", "请求体必须是有效 JSON。")
		return nil, false
	}
	object, ok := value.(map[string]any)
	if !ok {
		fail(c, 400, "INVALID_BODY", "请求体必须是 JSON 对象。")
		return nil, false
	}
	return object, true
}

func respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, task.ErrTitle):
		fail(c, 400, "INVALID_TITLE", err.Error())
	case errors.Is(err, task.ErrStatus):
		fail(c, 400, "INVALID_STATUS", err.Error())
	case errors.Is(err, task.ErrNotFound):
		fail(c, 404, "NOT_FOUND", err.Error())
	default:
		slog.Error("request failed", "error", err)
		fail(c, 500, "INTERNAL_ERROR", "服务器内部错误。")
	}
}

func New(store *task.Store) *gin.Engine {
	r := gin.New()
	r.RedirectTrailingSlash = false
	r.RedirectFixedPath = false
	r.Use(func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("request panic", "error", recovered)
				fail(c, 500, "INTERNAL_ERROR", "服务器内部错误。")
			}
		}()
		c.Next()
	})
	r.NoRoute(func(c *gin.Context) { fail(c, 404, "NOT_FOUND", "接口不存在。") })
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"data": gin.H{"status": "ok"}}) })
	r.GET("/tasks", func(c *gin.Context) { c.JSON(200, gin.H{"data": store.List()}) })
	r.POST("/tasks", func(c *gin.Context) {
		body, ok := readBody(c)
		if !ok {
			return
		}
		created, err := store.Create(body["title"])
		if err != nil {
			respondError(c, err)
			return
		}
		c.JSON(201, gin.H{"data": created})
	})
	r.PATCH("/tasks/:id", func(c *gin.Context) {
		body, ok := readBody(c)
		if !ok {
			return
		}
		updated, err := store.Update(c.Param("id"), body["status"])
		if err != nil {
			respondError(c, err)
			return
		}
		c.JSON(200, gin.H{"data": updated})
	})
	r.DELETE("/tasks/:id", func(c *gin.Context) {
		if err := store.Delete(c.Param("id")); err != nil {
			respondError(c, err)
			return
		}
		c.Status(204)
	})
	return r
}
