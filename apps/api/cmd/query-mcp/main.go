package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/querymcp"
)

func run() error {
	cfg, err := config.LoadQueryMCP()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	service, err := querymcp.Open(startup, cfg.DSN)
	cancel()
	if err != nil {
		return err
	}

	defer service.Close()
	server := &http.Server{Addr: cfg.Address, Handler: querymcp.NewHandler(service, cfg.Token), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second}
	done := make(chan error, 1)
	go func() {
		done <- server.ListenAndServe()
	}()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return errors.New("MCP HTTP 服务启动失败")
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
		slog.Error("query MCP stopped", "error", err)
		os.Exit(1)
	}
}
