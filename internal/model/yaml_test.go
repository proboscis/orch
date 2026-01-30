package model

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNeedsYAMLQuoting(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"simple", false},
		{"hello world", false},
		{"Test: with colon", true},
		{"has # hash", true},
		{"has [bracket", true},
		{"has ]bracket", true},
		{"has {brace", true},
		{"has }brace", true},
		{"has > angle", true},
		{"has | pipe", true},
		{"has * asterisk", true},
		{"has & ampersand", true},
		{"has ! exclamation", true},
		{"has % percent", true},
		{"has @ at", true},
		{"has ` backtick", true},
		{"has ' single quote", true},
		{"has \" double quote", true},
		{"has \\ backslash", true},
		{"has \n newline", true},
		{"- starts with dash", true},
		{"? starts with question", true},
		{" starts with space", true},
		{"ends with space ", true},
		{"", true},
		{"true", true},
		{"false", true},
		{"null", true},
		{"~", true},
		{"yes", true},
		{"no", true},
		{"on", true},
		{"off", true},
		{"True", true},
		{"False", true},
		{"YES", true},
		{"NO", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NeedsYAMLQuoting(tt.input)
			if got != tt.want {
				t.Errorf("NeedsYAMLQuoting(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestQuoteYAMLValue(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"hello world", "hello world"},
		{"Test: with colon", `"Test: with colon"`},
		{"has # hash", `"has # hash"`},
		{`has " quote`, `"has \" quote"`},
		{`has \ backslash`, `"has \\ backslash"`},
		{"has \n newline", `"has \n newline"`},
		{"- starts with dash", `"- starts with dash"`},
		{"Fix: login timeout", `"Fix: login timeout"`},
		{"", `""`},
		{"true", `"true"`},
		{"false", `"false"`},
		{"null", `"null"`},
		{"yes", `"yes"`},
		{"no", `"no"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := QuoteYAMLValue(tt.input)
			if got != tt.want {
				t.Errorf("QuoteYAMLValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestQuoteYAMLValue_RoundTrip(t *testing.T) {
	tests := []string{
		"simple",
		"hello world",
		"Test: with colon",
		"has # hash",
		"has [bracket]",
		"has {brace}",
		`has "quote"`,
		`has 'single'`,
		"has \n newline",
		"has \t tab",
		"- starts with dash",
		"ends with space ",
		" starts with space",
		"",
		"true",
		"false",
		"null",
		"~",
		"yes",
		"no",
		"on",
		"off",
		"True",
		"YES",
		"123",
		"1.5",
		"multi\nline\nvalue",
		"unicode: 日本語",
		`combo: "quoted" & special # chars`,
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			quoted := QuoteYAMLValue(input)
			yamlLine := "title: " + quoted + "\n"

			var out map[string]string
			if err := yaml.Unmarshal([]byte(yamlLine), &out); err != nil {
				t.Fatalf("yaml.Unmarshal failed for %q (quoted as %q): %v", input, quoted, err)
			}

			got := out["title"]
			if got != input {
				t.Errorf("round-trip failed:\n  input:  %q\n  quoted: %q\n  parsed: %q", input, quoted, got)
			}
		})
	}
}

func TestQuoteYAMLValue_FrontmatterRoundTrip(t *testing.T) {
	tests := []struct {
		id      string
		title   string
		summary string
	}{
		{"simple-id", "Simple Title", "Simple summary"},
		{"id-123", "Fix: colon in title", "Summary: also has colon"},
		{"test", "true", "false"},
		{"id", "Title with # hash", ""},
		{"dash-id", "- starts with dash", "ends with space "},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			frontmatter := "---\n" +
				"type: issue\n" +
				"id: " + QuoteYAMLValue(tt.id) + "\n" +
				"title: " + QuoteYAMLValue(tt.title) + "\n"
			if tt.summary != "" {
				frontmatter += "summary: " + QuoteYAMLValue(tt.summary) + "\n"
			}
			frontmatter += "status: open\n" +
				"---\n"

			var out map[string]interface{}
			if err := yaml.Unmarshal([]byte(frontmatter), &out); err != nil {
				t.Fatalf("yaml.Unmarshal failed: %v\nfrontmatter:\n%s", err, frontmatter)
			}

			if got := out["id"]; got != tt.id {
				t.Errorf("id mismatch: got %q, want %q", got, tt.id)
			}
			if got := out["title"]; got != tt.title {
				t.Errorf("title mismatch: got %q, want %q", got, tt.title)
			}
			if tt.summary != "" {
				if got := out["summary"]; got != tt.summary {
					t.Errorf("summary mismatch: got %q, want %q", got, tt.summary)
				}
			}
		})
	}
}
