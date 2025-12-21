package cli

import (
	"testing"
	"time"
)

func TestFormatRelativeTime(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "just now - 0 seconds",
			duration: 0,
			expected: "just now",
		},
		{
			name:     "just now - 30 seconds",
			duration: 30 * time.Second,
			expected: "just now",
		},
		{
			name:     "just now - 59 seconds",
			duration: 59 * time.Second,
			expected: "just now",
		},
		{
			name:     "1 minute ago",
			duration: time.Minute,
			expected: "1m ago",
		},
		{
			name:     "5 minutes ago",
			duration: 5 * time.Minute,
			expected: "5m ago",
		},
		{
			name:     "59 minutes ago",
			duration: 59 * time.Minute,
			expected: "59m ago",
		},
		{
			name:     "1 hour ago",
			duration: time.Hour,
			expected: "1h ago",
		},
		{
			name:     "2 hours ago",
			duration: 2 * time.Hour,
			expected: "2h ago",
		},
		{
			name:     "23 hours ago",
			duration: 23 * time.Hour,
			expected: "23h ago",
		},
		{
			name:     "1 day ago",
			duration: 24 * time.Hour,
			expected: "1d ago",
		},
		{
			name:     "3 days ago",
			duration: 3 * 24 * time.Hour,
			expected: "3d ago",
		},
		{
			name:     "6 days ago",
			duration: 6 * 24 * time.Hour,
			expected: "6d ago",
		},
		{
			name:     "1 week ago",
			duration: 7 * 24 * time.Hour,
			expected: "1w ago",
		},
		{
			name:     "2 weeks ago",
			duration: 14 * 24 * time.Hour,
			expected: "2w ago",
		},
		{
			name:     "10 weeks ago",
			duration: 70 * 24 * time.Hour,
			expected: "10w ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testTime := time.Now().Add(-tt.duration)
			result := formatRelativeTime(testTime)
			if result != tt.expected {
				t.Errorf("formatRelativeTime() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatRelativeTimeFuture(t *testing.T) {
	// Future times should show as "just now"
	futureTime := time.Now().Add(10 * time.Minute)
	result := formatRelativeTime(futureTime)
	if result != "just now" {
		t.Errorf("formatRelativeTime(future) = %q, want %q", result, "just now")
	}
}
