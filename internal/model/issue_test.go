package model

import "testing"

func TestPathSafeIssueID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gh#285", "gh-285"},
		{"gh#1", "gh-1"},
		{"gh#12345", "gh-12345"},
		{"plc124", "plc124"},
		{"issue-with-dash", "issue-with-dash"},
		{"no-hash", "no-hash"},
		{"", ""},
		{"#123", "-123"},
		{"multiple#hash#chars", "multiple-hash-chars"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := PathSafeIssueID(tt.input)
			if got != tt.want {
				t.Errorf("PathSafeIssueID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsGitHubIssueID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"gh#285", true},
		{"gh#1", true},
		{"#123", true},
		{"123", true},
		{"plc124", false},
		{"issue-123", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsGitHubIssueID(tt.input)
			if got != tt.want {
				t.Errorf("IsGitHubIssueID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeGitHubIssueID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gh#285", "gh#285"},
		{"#123", "gh#123"},
		{"123", "gh#123"},
		{"plc124", "plc124"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeGitHubIssueID(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeGitHubIssueID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseGitHubIssueNumber(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"gh#285", 285, false},
		{"#123", 123, false},
		{"456", 456, false},
		{"invalid", 0, true},
		{"gh#abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseGitHubIssueNumber(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGitHubIssueNumber(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseGitHubIssueNumber(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
