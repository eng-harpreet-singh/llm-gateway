package router

import "testing"

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
		{"llama3.1", "ollama"},
		{"mistral", "ollama"},
		{"gemma2", "ollama"},
		{"qwen2.5", "ollama"},
		{"phi3", "ollama"},
		// Unknown: empty string, so Route falls back to the default
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