package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/eng-harpreet-singh/llm-gateway/internal/config"
	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
	"github.com/eng-harpreet-singh/llm-gateway/internal/router"
	"github.com/eng-harpreet-singh/llm-gateway/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// real logic lives here so it can return an error; main owns the exit path
func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// today: just OpenAI. tomorrow: one more line for Anthropic + one new file.
	openai := provider.NewOpenAIProvider(cfg.OpenAIAPIKey, cfg.OpenAIBaseURL)

	rtr, err := router.New(openai)
	if err != nil {
		return err
	}

	handler := server.NewHandler(rtr, logger, cfg.DefaultModel)
	srv := server.New(":"+cfg.Port, handler.Routes(), logger, cfg.ShutdownTimeout)

	// context that cancels on SIGINT/SIGTERM -> graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return srv.Run(ctx)
}