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
			got := IsGitHubIssueID(NewIssueID(tt.input))
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
			got := NormalizeGitHubIssueID(NewIssueID(tt.input))
			if got.String() != tt.want {
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
			got, err := ParseGitHubIssueNumber(NewIssueID(tt.input))
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

func TestParseTags(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"bug", []string{"bug"}},
		{"bug, urgent", []string{"bug", "urgent"}},
		{"bug,urgent", []string{"bug", "urgent"}},
		{"[bug, urgent]", []string{"bug", "urgent"}},
		{"[bug,urgent]", []string{"bug", "urgent"}},
		{" bug , urgent ", []string{"bug", "urgent"}},
		{"[enhancement, tui, ux]", []string{"enhancement", "tui", "ux"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseTags(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("ParseTags(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("ParseTags(%q)[%d] = %q, want %q", tt.input, i, v, tt.want[i])
				}
			}
		})
	}
}

func TestFormatTags(t *testing.T) {
	tests := []struct {
		input []string
		want  string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"bug"}, "[bug]"},
		{[]string{"bug", "urgent"}, "[bug, urgent]"},
		{[]string{"enhancement", "tui", "ux"}, "[enhancement, tui, ux]"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatTags(tt.input)
			if got != tt.want {
				t.Errorf("FormatTags(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
