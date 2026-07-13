package cli

import (
	"strings"
	"testing"
)

func TestRepairHelpDescribesImplementedSessionReporting(t *testing.T) {
	help := newRepairCmd().Long
	if strings.Contains(help, "worktrees") {
		t.Fatalf("repair help still promises unimplemented worktree handling: %q", help)
	}
	for _, phrase := range []string{"orphaned", "terminal-but-alive", "unreapable-kept"} {
		if !strings.Contains(help, phrase) {
			t.Fatalf("repair help missing %q: %q", phrase, help)
		}
	}
}
