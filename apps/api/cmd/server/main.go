package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"github.com/roychen-7/codex-tester/apps/api/internal/chat"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/database"
	"github.com/roychen-7/codex-tester/apps/api/internal/handler"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
)

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})))
	chatCfg, err := config.LoadChat()
	if err != nil {
		return err
	}

	dbCtx, dbCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer dbCancel()

	db, err := database.Open(dbCtx, chatCfg.DSN)
	if err != nil {
		return err
	}

	pool, _ := db.DB()
	defer pool.Close()
	if err = database.Ready(dbCtx, db); err != nil {
		return errors.New("请先执行数据库迁移")
	}

	llmCfg, err := config.LoadLLM()
	if err != nil {
		return err
	}

	agentCfg, err := config.LoadAgent()
	if err != nil {
		return err
	}

	store := &chat.Store{
		DB: db, LLMModel: llmCfg.Model,
		ConversationMode:  agentCfg.Mode == "conversation",
		QueryToolsEnabled: llmCfg.APIKey != "" && agentCfg.MCPURL != "",
		AnalysisEnabled:   agentCfg.Mode == "legacy" && llmCfg.APIKey != "" && agentCfg.MCPURL != "",
	}
	gin.SetMode(gin.ReleaseMode)
	server := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler:           handler.NewChat(store, chatCfg),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()
	if llmCfg.APIKey != "" {
		workerDone := make(chan struct{})
		go func() {
			defer close(workerDone)
			model := llm.NewOpenAI(llmCfg)
			model.AnalysisAvailable = store.AnalysisEnabled
			if store.ConversationMode {
				conversation := &agent.Conversation{Model: llm.DialogueRounder{Model: model}, Config: agentCfg}
				store.WorkDialogue(workerCtx, conversation, 10*time.Minute)
			} else {
				store.Work(workerCtx, model, llmCfg.Timeout)
			}
		}()
		defer func() {
			workerCancel()
			<-workerDone
		}()
	}

	if store.AnalysisEnabled {
		analysisDone := make(chan struct{})
		go func() {
			defer close(analysisDone)
			store.WorkAnalysis(workerCtx, llm.NewOpenAI(llmCfg), agentCfg)
		}()
		defer func() {
			workerCancel()
			<-analysisDone
		}()
	}

	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
	}()

	slog.Info("API started", "url", "http://"+server.Addr, "environment", cfg.Environment)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}

		return nil
	}
}

func main() {
	if err := run(); err != nil {
		slog.Error("API stopped", "error", err)
		os.Exit(1)
	}
}
