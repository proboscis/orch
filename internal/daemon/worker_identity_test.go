package daemon

import "testing"

func TestDefaultWorkerIDUsesStableHostIdentity(t *testing.T) {
	orig := currentHostname
	currentHostname = func() (string, error) { return "zeus.example", nil }
	t.Cleanup(func() { currentHostname = orig })

	if got := defaultWorkerID(); got != "host-zeus.example" {
		t.Fatalf("defaultWorkerID() = %q, want %q", got, "host-zeus.example")
	}
}
