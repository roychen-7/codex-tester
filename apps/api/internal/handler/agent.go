package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/roychen-7/codex-tester/apps/api/internal/chat"
)

func agentRoutes(conversation *gin.RouterGroup, store *chat.Store) {
	user := func(c *gin.Context) uint64 {
		return c.MustGet("user").(chat.User).ID
	}

	conversation.GET("/agent", func(c *gin.Context) {
		state, err := store.AgentState(c.Request.Context(), user(c), c.Param("id"))
		if err != nil {
			chatError(c, err)
			return
		}

		c.JSON(200, gin.H{"data": state})
	})

	conversation.POST("/tasks/:task/confirm", func(c *gin.Context) {
		var input struct {
			Revision uint32 `json:"revision"`
		}
		if !decodeChat(c, &input) {
			return
		}

		if !chat.ValidID(c.Param("task")) || input.Revision == 0 {
			chatError(c, chat.ErrInput)
			return
		}

		task, err := store.Confirm(c.Request.Context(), user(c), c.Param("id"), c.Param("task"), input.Revision)
		if err != nil {
			chatError(c, err)
			return
		}

		c.JSON(200, gin.H{"data": task})
	})

	conversation.POST("/runs/:run/retry", func(c *gin.Context) {
		var input struct{}
		if !decodeChat(c, &input) {
			return
		}

		if !chat.ValidID(c.Param("run")) {
			chatError(c, chat.ErrInput)
			return
		}

		run, err := store.Retry(c.Request.Context(), user(c), c.Param("id"), c.Param("run"))
		if err != nil {
			chatError(c, err)
			return
		}

		c.JSON(200, gin.H{"data": run})
	})
}
