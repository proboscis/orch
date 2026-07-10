package orchapi

import (
	"errors"
	"fmt"
	"testing"
)

func TestMapAmbiguousRefErrorSupportsCurrentAndHistoricDaemonMessages(t *testing.T) {
	tests := []string{
		"ambiguous run ref: orch-123#run-1",
		"ambiguous short id: abc123",
		"ambiguous_run_ref",
		"ambiguous_short_id",
	}

	for _, message := range tests {
		t.Run(message, func(t *testing.T) {
			err := fmt.Errorf("daemon error: %s", message)
			if got := mapAmbiguousRefError(err); !errors.Is(got, ErrAmbiguousRef) {
				t.Fatalf("mapAmbiguousRefError(%q) = %v, want ErrAmbiguousRef", err, got)
			}
		})
	}
}
