package router

import (
	"context"
	"errors"
	"testing"

	"github.com/eng-harpreet-singh/llm-gateway/internal/provider"
)

func TestProviderNameForModel(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		// OpenAI
		{"gpt-4o", "openai"},
		{"gpt-4o-mini", "openai"},
		{"o1-preview", "openai"},
		{"o3-mini", "openai"},
		// Anthropic
		{"claude-sonnet-4-5", "anthropic"},
		{"claude-haiku-4-5", "anthropic"},
		// Ollama (local model families)
		{"llama3.2", "ollama"},
		{"mistral", "ollama"},
		{"gemma2", "ollama"},
		{"qwen2.5", "ollama"},
		{"phi3", "ollama"},
		// Unknown family
		{"unknown-model", ""},
		{"", ""},
	}

	for _, c := range cases {
		t.Run(c.model, func(t *testing.T) {
			if got := providerNameForModel(c.model); got != c.want {
				t.Errorf("providerNameForModel(%q) = %q, want %q", c.model, got, c.want)
			}
		})
	}
}

func TestNormalizeModel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"gpt-4o", "gpt-4o"},
		{"  gpt-4o  ", "gpt-4o"}, // trims spaces
		{"GPT-4o", "gpt-4o"},     // lowercases
		{" Claude-3 ", "claude-3"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := normalizeModel(c.in); got != c.want {
				t.Errorf("normalizeModel(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// fakeProvider is a stand-in so we can build a Router without real adapters.
type fakeProvider struct{ name string }

func (f fakeProvider) Name() string { return f.name }
func (f fakeProvider) Complete(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{Provider: f.name}, nil
}

func TestRoute(t *testing.T) {
	openai := fakeProvider{name: "openai"}
	anthropic := fakeProvider{name: "anthropic"}
	r, err := New(openai, anthropic)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Run("known model routes to its provider", func(t *testing.T) {
		p, err := r.Route(provider.Request{Model: "gpt-4o"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name() != "openai" {
			t.Errorf("got %q, want openai", p.Name())
		}
	})

	t.Run("case-insensitive routing", func(t *testing.T) {
		p, err := r.Route(provider.Request{Model: " CLAUDE-3 "})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name() != "anthropic" {
			t.Errorf("got %q, want anthropic", p.Name())
		}
	})

	t.Run("empty model errors", func(t *testing.T) {
		_, err := r.Route(provider.Request{Model: ""})
		if !errors.Is(err, ErrNoProvider) {
			t.Errorf("got %v, want ErrNoProvider", err)
		}
	})

	t.Run("unknown model errors", func(t *testing.T) {
		_, err := r.Route(provider.Request{Model: "gtp-4o-typo"})
		if !errors.Is(err, ErrNoProvider) {
			t.Errorf("got %v, want ErrNoProvider", err)
		}
	})

	t.Run("known model but provider not registered errors", func(t *testing.T) {
		// "llama*" maps to ollama, but we only registered openai + anthropic.
		_, err := r.Route(provider.Request{Model: "llama3.2"})
		if !errors.Is(err, ErrNoProvider) {
			t.Errorf("got %v, want ErrNoProvider", err)
		}
	})
}