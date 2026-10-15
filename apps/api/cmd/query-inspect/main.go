package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/querymcp"
)

func run() error {
	dsn, err := config.LoadQueryPostgresDSN()
	if err != nil {
		return err
	}

	// Bound the driver's initial automatic ping as well as catalog queries.
	u, err := url.Parse(dsn)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("connect_timeout", "10")
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	service, err := querymcp.Open(ctx, u.String())
	if err != nil {
		return err
	}
	defer service.Close()

	report, err := service.Inspect(ctx)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func main() {
	if err := run(); err != nil {
		slog.Error("query inspection failed", "error", err)
		os.Exit(1)
	}
}
