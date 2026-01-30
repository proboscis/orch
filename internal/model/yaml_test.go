package model

import "testing"

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
		{"", false},
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
