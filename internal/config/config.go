package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all runtime config, loaded once from the environment.
type Config struct {
	Port            string
	OpenAIAPIKey    string
	OpenAIBaseURL   string
	DefaultModel    string
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration
}

// Load reads env, applies defaults, fails fast if a required value is missing.
func Load() (Config, error) {
	cfg := Config{
		Port:            getEnv("PORT", "8080"),
		OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL:   getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		DefaultModel:    getEnv("DEFAULT_MODEL", "gpt-4o-mini"),
		RequestTimeout:  getDuration("REQUEST_TIMEOUT", 30*time.Second),
		ShutdownTimeout: getDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
	}
	if cfg.OpenAIAPIKey == "" {
		return Config{}, fmt.Errorf("config: OPENAI_API_KEY is required")
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