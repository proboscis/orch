package cli

import (
	"fmt"
	"strings"
	"testing"
)

func TestFormatRunsSummary(t *testing.T) {
	summary := formatRunsSummary([]runSummary{
		{RunID: "1", Status: "running"},
		{RunID: "2", Status: "blocked"},
		{RunID: "3", Status: "running"},
	})

	parts := strings.Split(summary, ", ")
	counts := make(map[string]int)
	for _, part := range parts {
		var count int
		var status string
		if _, err := fmt.Sscanf(part, "%d %s", &count, &status); err != nil {
			t.Fatalf("parse %q: %v", part, err)
		}
		counts[status] = count
	}

	if counts["running"] != 2 || counts["blocked"] != 1 {
		t.Fatalf("unexpected counts: %#v", counts)
	}
}
