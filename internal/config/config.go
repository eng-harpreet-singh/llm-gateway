// Package config loads all runtime settings once from the environment.
// It applies defaults and fails fast if a required value is missing.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

// Defaults for scorer and advisory tuning. Threshold and currency can be
// overridden by env; signal words stay a code default (they rarely change).
const (
	defaultComplexityThreshold = 1000
	defaultCurrency            = "USD"
	defaultRedisAddr = "localhost:6379"
	defaultRPMLimit  = 60      // requests per minute per tenant
	defaultTPMLimit  = 100_000 // tokens per minute per tenant
	defaultPostgresURL = "postgres://gateway:gateway@localhost:5432/gateway"
)

// defaultSignalWords push a prompt to the premium tier when matched.
var defaultSignalWords = []string{
	"analyze", "explain", "reason", "prove", "step by step", "evaluate", "compare",
}

// ModelOption is one model the gateway can route to: its tier, provider,
// the model name the provider expects, and its input price.
type ModelOption struct {
	Tier             string
	Provider         string
	Model            string
	PricePer1MInput  float64
	PricePer1MOutput float64 // price per 1M output tokens
}

// Models is the catalogue the advisory layer shows and the router routes to.
// A slice, so adding a model later is one more entry, no code change.
type Models []ModelOption

// Config holds all runtime config, loaded once from the environment.
type Config struct {
	Port             string
	OpenAIAPIKey     string
	OpenAIBaseURL    string
	OllamaBaseURL    string
	AnthropicAPIKey  string
	AnthropicBaseURL string
	DefaultModel     string
	RequestTimeout   time.Duration
	ShutdownTimeout  time.Duration
	RedisAddr string
	PostgresURL string
	RPMLimit  int
	TPMLimit  int

	// Scorer and advisory tuning.
	ComplexityThreshold int
	SignalWords         []string
	Currency            string

	Models Models // model catalogue (tier, provider, price) for routing + advisory
}

// Load reads env, applies defaults, and fails fast if a required value
// is missing. Called once at startup.
func Load() (Config, error) {
	cfg := Config{
		Port:             getEnv("PORT", "8080"),
		OpenAIAPIKey:     os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL:    getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		AnthropicBaseURL: getEnv("ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
		DefaultModel:     getEnv("DEFAULT_MODEL", "gpt-4o-mini"),
		RequestTimeout:   getDuration("REQUEST_TIMEOUT", 30*time.Second),
		ShutdownTimeout:  getDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		OllamaBaseURL:    getEnv("OLLAMA_BASE_URL", "http://localhost:11434"),

		ComplexityThreshold: getInt("COMPLEXITY_THRESHOLD", defaultComplexityThreshold),
		SignalWords:         defaultSignalWords,
		Currency:            getEnv("CURRENCY", defaultCurrency),

		RedisAddr: getEnv("REDIS_ADDR", defaultRedisAddr),
		RPMLimit:  getInt("RPM_LIMIT", defaultRPMLimit),
		TPMLimit:  getInt("TPM_LIMIT", defaultTPMLimit),
		PostgresURL: getEnv("POSTGRES_URL", defaultPostgresURL),
	}

	// OpenAI key is required because the local tokenizer and default model
	// both lean on it; fail fast so we don't start half-broken.
	if cfg.OpenAIAPIKey == "" {
		return Config{}, fmt.Errorf("config: OPENAI_API_KEY is required")
	}

	// Curated model catalogue. Two tiers, two providers each. Add more later
	// by appending entries — no code change needed elsewhere.
	cfg.Models = Models{
		{Tier: "affordable", Provider: provider.NameOpenAI, Model: "gpt-4o-mini", PricePer1MInput: 0.15, PricePer1MOutput: 0.60},
		{Tier: "affordable", Provider: provider.NameAnthropic, Model: "claude-haiku-4-5", PricePer1MInput: 0.25, PricePer1MOutput: 1.25},
		{Tier: "premium", Provider: provider.NameOpenAI, Model: "gpt-4o", PricePer1MInput: 2.50, PricePer1MOutput: 10.00},
		{Tier: "premium", Provider: provider.NameAnthropic, Model: "claude-sonnet-4-5", PricePer1MInput: 3.00, PricePer1MOutput: 15.00},
		{Tier: "local", Provider: provider.NameOllama, Model: "llama3.2", PricePer1MInput: 0.0, PricePer1MOutput: 0.0},
	}

	return cfg, nil
}

// getEnv returns the env value for key, or fallback if unset.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getDuration parses a duration env value (e.g. "30s"), or returns fallback.
func getDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

// getInt parses an integer env value, or returns fallback on missing/bad input.
func getInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// getFloat parses a float env value, or returns fallback on missing/bad input.
func getFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}