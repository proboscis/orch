package monitor

import (
	"testing"
	"time"
)

func TestParseFilterDuration(t *testing.T) {
	tests := []struct {
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{raw: "24h", want: 24 * time.Hour},
		{raw: "7d", want: 7 * 24 * time.Hour},
		{raw: "2w", want: 14 * 24 * time.Hour},
		{raw: "0h", wantErr: true},
		{raw: "bad", wantErr: true},
	}

	for _, tt := range tests {
		got, err := parseFilterDuration(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseFilterDuration(%q) expected error, got nil", tt.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseFilterDuration(%q) unexpected error: %v", tt.raw, err)
		}
		if got != tt.want {
			t.Fatalf("parseFilterDuration(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}

func TestCompileIssueQuery(t *testing.T) {
	re, isRegex, err := compileIssueQuery("/orch-\\d+/")
	if err != nil {
		t.Fatalf("compileIssueQuery returned error: %v", err)
	}
	if !isRegex {
		t.Fatal("compileIssueQuery expected regex true")
	}
	if re == nil || !re.MatchString("orch-123") {
		t.Fatal("compiled regex did not match expected input")
	}
	if re.MatchString("misc") {
		t.Fatal("compiled regex matched unexpected input")
	}
}
