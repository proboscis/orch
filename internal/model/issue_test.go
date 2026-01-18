package model

import "testing"

func TestIsGitHubIssueID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"gh-123", true},
		{"gh-1", true},
		{"gh#123", true},
		{"gh#1", true},
		{"#123", true},
		{"123", true},
		{"local-issue", false},
		{"plc124", false},
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
		{"gh-123", "gh-123"},
		{"gh#123", "gh-123"},
		{"#123", "gh-123"},
		{"123", "gh-123"},
		{"local-issue", "local-issue"},
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
		{"gh-123", 123, false},
		{"gh#123", 123, false},
		{"#123", 123, false},
		{"123", 123, false},
		{"gh-abc", 0, true},
		{"gh#abc", 0, true},
		{"local-issue", 0, true},
		{"", 0, true},
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
