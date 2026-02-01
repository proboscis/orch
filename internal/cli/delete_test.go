package cli

import (
	"testing"
	"time"

	"github.com/s22625/orch/internal/model"
)

func TestDurationToOlderThan(t *testing.T) {
	cases := []struct {
		input        string
		wantDuration time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"1m", 30 * 24 * time.Hour},
	}

	for _, tc := range cases {
		got, err := durationToOlderThan(tc.input)
		if err != nil {
			t.Fatalf("durationToOlderThan(%q) error: %v", tc.input, err)
		}

		parsedTime, err := time.Parse(time.RFC3339, got)
		if err != nil {
			t.Fatalf("durationToOlderThan(%q) returned invalid RFC3339: %v", tc.input, err)
		}

		expectedCutoff := time.Now().Add(-tc.wantDuration)
		diff := parsedTime.Sub(expectedCutoff)
		if diff < -time.Second || diff > time.Second {
			t.Fatalf("durationToOlderThan(%q) = %v, want ~%v (diff: %v)", tc.input, parsedTime, expectedCutoff, diff)
		}
	}
}

func TestDurationToOlderThanInvalid(t *testing.T) {
	if _, err := durationToOlderThan("7x"); err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestParseStatus(t *testing.T) {
	if _, err := parseStatus(string(model.StatusDone)); err != nil {
		t.Fatalf("parseStatus done error: %v", err)
	}
	if _, err := parseStatus(string(model.StatusRunning)); err == nil {
		t.Fatal("expected error for running status")
	}
}
