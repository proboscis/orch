package model

import "testing"

func TestShortModelAlias(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"gpt-5.2-codex", "5.2-codex"},
		{"gpt-4o-mini", "4o-mini"},
		{"claude-3-5-sonnet-20241022", "3.5-sonnet"},
		{"claude-3-opus-latest", "3-opus"},
		{"claude-sonnet-4-5-20250929", "4.5-sonnet"},
		{"gemini-1.5-pro", "1.5-pro"},
		{"o3", "o3"},
		{"", ""},
	}

	for _, tc := range cases {
		if got := ShortModelAlias(tc.input); got != tc.want {
			t.Fatalf("ShortModelAlias(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestModelThinkingAlias(t *testing.T) {
	got := ModelThinkingAlias("gpt-5.2-codex", "xhigh")
	if got != "5.2-codex-xhigh" {
		t.Fatalf("ModelThinkingAlias = %q, want %q", got, "5.2-codex-xhigh")
	}
}
