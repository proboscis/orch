package agent

import "testing"

func TestAgentDisplayName(t *testing.T) {
	tests := []struct {
		agent   string
		model   string
		variant string
		want    string
	}{
		{"opencode", "anthropic/claude-opus-4-5", "max", "oc:opus4.5"},
		{"opencode", "anthropic/claude-opus-4-5", "", "oc:opus4.5"},
		{"opencode", "anthropic/claude-opus-4-5", "high", "oc:opus4.5h"},
		{"opencode", "anthropic/claude-sonnet-4-5", "", "oc:sonnet4.5"},
		{"opencode", "openai/gpt-5-2", "", "oc:gpt5.2"},
		{"opencode", "openai/gpt-5-2", "codex", "oc:gpt5.2c"},
		{"opencode", "openai/o3", "", "oc:o3"},
		{"opencode", "openai/o4-mini", "", "oc:o4-mini"},
		{"opencode", "google/gemini-3-pro", "", "oc:gemini3-pro"},
		{"opencode", "google/gemini-2-0-flash", "", "oc:gemini2-0-flash"},
		{"opencode", "", "", "oc"},
		{"claude", "", "", "claude"},
		{"codex", "", "", "codex"},
		{"gemini", "", "", "gemini"},
		{"", "", "", "-"},
		{"  opencode  ", "  anthropic/claude-opus-4-5  ", "  max  ", "oc:opus4.5"},
	}

	for _, tt := range tests {
		t.Run(tt.agent+"/"+tt.model+"/"+tt.variant, func(t *testing.T) {
			got := AgentDisplayName(tt.agent, tt.model, tt.variant)
			if got != tt.want {
				t.Errorf("AgentDisplayName(%q, %q, %q) = %q, want %q",
					tt.agent, tt.model, tt.variant, got, tt.want)
			}
		})
	}
}

func TestShortenModelName(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		{"anthropic/claude-opus-4-5", "opus4.5"},
		{"anthropic/claude-sonnet-4-5", "sonnet4.5"},
		{"claude-opus-4-5", "opus4.5"},
		{"openai/gpt-5-2", "gpt5.2"},
		{"gpt-5-2", "gpt5.2"},
		{"openai/o3", "o3"},
		{"openai/o4-mini", "o4-mini"},
		{"google/gemini-3-pro", "gemini3-pro"},
		{"google/gemini-2-0-flash", "gemini2-0-flash"},
		{"unknown-model", "unknown-model"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := shortenModelName(tt.model)
			if got != tt.want {
				t.Errorf("shortenModelName(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

func TestVariantSuffix(t *testing.T) {
	tests := []struct {
		variant string
		want    string
	}{
		{"", ""},
		{"max", ""},
		{"high", "h"},
		{"codex", "c"},
		{"mini", "m"},
		{"low", "l"},
		{"unknown", "u"},
	}

	for _, tt := range tests {
		t.Run(tt.variant, func(t *testing.T) {
			got := variantSuffix(tt.variant)
			if got != tt.want {
				t.Errorf("variantSuffix(%q) = %q, want %q", tt.variant, got, tt.want)
			}
		})
	}
}
