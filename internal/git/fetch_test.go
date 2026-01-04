package git

import (
	"os"
	"testing"
	"time"
)

func TestFetchWithTimeout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fetch-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	err = FetchWithTimeout("/nonexistent/path", "", 1*time.Second)
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestFetchWithTimeoutShortDuration(t *testing.T) {
	err := FetchWithTimeout(".", "", 1*time.Nanosecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
