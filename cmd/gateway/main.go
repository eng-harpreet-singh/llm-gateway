// Command gateway is the entry point and composition root for the LLM gateway.
//
// main does exactly one job well: it is the ONLY place that knows how the
// pieces fit together. It loads config, constructs concrete dependencies, wires
// them, and starts the server. Everything below main depends on interfaces, not
// on each other's construction — which is what keeps the system testable and
// the dependency graph a tree, not a web.
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
	// Structured logging (slog, stdlib since 1.21). JSON handler so logs are
	// machine-parseable for the observability stack we add later.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// run holds the real logic so it can return an error (main can't). This pattern
// — thin main, fallible run — keeps the exit-code path in one obvious place.
func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Construct concrete providers. Each is one line + one file; nothing else
	// changes when we add more.
	openai := provider.NewOpenAIProvider(cfg.OpenAIAPIKey, cfg.OpenAIBaseURL)
	anthropic := provider.NewAnthropicProvider(cfg.AnthropicAPIKey, cfg.AnthropicBaseURL)
	ollama := provider.NewOllamaProvider(cfg.OllamaBaseURL)

	rtr, err := router.New(openai, anthropic, ollama)
	if err != nil {
		return err
	}

	// Token counter for the advisory cost estimate. We use the OpenAI local
	// counter (tiktoken, no network) so advising stays fast — exact-enough for
	// a pre-flight estimate, and it never blocks on an API call.
	counter := token.NewOpenAICounter()

	// Complexity scorer: tier decision from cheap signals (length + keywords).
	// Threshold and signal words are wired here; move to config later if needed.
	signalWords := []string{"analyze", "explain", "reason", "prove", "step by step", "evaluate", "compare"}
	scorer := router.NewHeuristicScorer(1000, signalWords)

	// Advisor ties scorer + counter + the model catalogue together to produce
	// a cost/tier recommendation. Currency is display-only arithmetic.
	advisor := router.NewAdvisor(scorer, counter, cfg.Models, "USD")

	handler := server.NewHandler(rtr, advisor, logger, cfg.DefaultModel)
	srv := server.New(":"+cfg.Port, handler.Routes(), logger, cfg.ShutdownTimeout)

	// signal.NotifyContext gives us a context that cancels on SIGINT/SIGTERM —
	// the idiomatic modern way to wire OS signals to graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return srv.Run(ctx)
}