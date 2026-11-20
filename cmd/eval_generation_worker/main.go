package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/eval-hub/eval-hub/internal/eval_hub/generation/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	path := os.Getenv("EVALHUB_JOB_SPEC_PATH")
	if path == "" {
		path = worker.DefaultJobSpecPath
	}
	if err := worker.Run(ctx, path, nil); err != nil {
		// Errors intentionally omit request/model/spec contents and response bodies.
		slog.Error("collection-generation worker failed", "error", err)
		os.Exit(1)
	}
}
