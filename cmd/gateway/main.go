// Command gateway is the entry point and composition root for the LLM gateway.
//
// main does one job: it is the ONLY place that knows how the pieces fit
// together. It loads config, builds concrete dependencies, wires them, and
// starts the server. Everything below main depends on interfaces, not on each
// other's construction — keeping the system testable and the graph a tree.
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
	"github.com/eng-harpreet-singh/llm-gateway/internal/token"
)

func main() {
	// Structured JSON logging so logs are machine-parseable for the
	// observability stack we add later.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// run holds the real logic so it can return an error (main can't).
// Thin main, fallible run — keeps the exit-code path in one place.
func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Build concrete providers. Each is one line and one file; adding a
	// provider changes nothing else here.
	openai := provider.NewOpenAIProvider(cfg.OpenAIAPIKey, cfg.OpenAIBaseURL)
	anthropic := provider.NewAnthropicProvider(cfg.AnthropicAPIKey, cfg.AnthropicBaseURL)
	ollama := provider.NewOllamaProvider(cfg.OllamaBaseURL)

	rtr, err := router.New(openai, anthropic, ollama)
	if err != nil {
		return err
	}

	// Local tokenizer (tiktoken, no network) for the advisory cost estimate,
	// so advising stays fast and never blocks on an API call.
	counter := token.NewOpenAICounter()

	// Complexity scorer for tier selection. Threshold and signal words come
	// from config now, not hardcoded here.
	scorer := router.NewHeuristicScorer(cfg.ComplexityThreshold, cfg.SignalWords)

	// Advisor ties scorer + counter + catalogue into a cost/tier recommendation.
	// Currency is display-only arithmetic.
	advisor := router.NewAdvisor(scorer, counter, cfg.Models, cfg.Currency)

	handler := server.NewHandler(rtr, advisor, logger, cfg.DefaultModel)
	srv := server.New(":"+cfg.Port, handler.Routes(), logger, cfg.ShutdownTimeout)

	// signal.NotifyContext cancels the context on SIGINT/SIGTERM — the modern
	// way to wire OS signals to graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return srv.Run(ctx)
}