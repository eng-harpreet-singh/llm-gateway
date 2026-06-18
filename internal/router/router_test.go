package router

import "testing"

func TestProviderNameForModel(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"gpt-4o-mini", "openai"},
		{"gpt-4o", "openai"},
		{"o1-preview", "openai"},
		{"claude-sonnet-4-5", "anthropic"},
		{"claude-3-5-haiku", "anthropic"},
		{"unknown-model", ""},
	}
	for _, c := range cases {
		if got := providerNameForModel(c.model); got != c.want {
			t.Errorf("providerNameForModel(%q) = %q, want %q", c.model, got, c.want)
		}
	}
}