package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// ModelOption is one model the gateway can route to: its tier, provider,
// the model name the provider expects, and its input price.
// Price is in currency units per 1M input tokens (the gateway just does the
// arithmetic; the unit — dollars, rupees — is your choice).
type ModelOption struct {
	Tier            string
	Provider        string
	Model           string
	PricePer1MInput float64
}

// Models is the catalogue the advisory layer shows and the router routes to.
// It's a slice so adding a model later is one more entry, no code change.
type Models []ModelOption

// Config holds all runtime config, loaded once from the environment.
type Config struct {
	Port             string
	OpenAIAPIKey     string
	OpenAIBaseURL    string
	AnthropicAPIKey  string
	AnthropicBaseURL string
	DefaultModel     string
	RequestTimeout   time.Duration
	ShutdownTimeout  time.Duration

	Models Models // model catalogue (tier, provider, price) for routing + advisory
}

// Load reads env, applies defaults, fails fast if a required value is missing.
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
	}
	if cfg.OpenAIAPIKey == "" {
		return Config{}, fmt.Errorf("config: OPENAI_API_KEY is required")
	}

	// Curated model catalogue. Two tiers, two providers each. Add more later
	// by appending entries — no code change needed elsewhere.
	cfg.Models = Models{
		{Tier: "affordable", Provider: "openai", Model: "gpt-4o-mini", PricePer1MInput: 0.15},
		{Tier: "affordable", Provider: "anthropic", Model: "claude-haiku-4-5", PricePer1MInput: 0.25},
		{Tier: "premium", Provider: "openai", Model: "gpt-4o", PricePer1MInput: 2.50},
		{Tier: "premium", Provider: "anthropic", Model: "claude-sonnet-4-5", PricePer1MInput: 3.00},
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func getFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}