package daemon

import "testing"

// ADR-0003: the daemon's unauthenticated TCP control socket binds loopback
// by default; multi-host exposure is an explicit --listen opt-in.
func TestDefaultTCPListenAddrIsLoopback(t *testing.T) {
	if !isLoopbackListenAddr(DefaultTCPListenAddr) {
		t.Fatalf("DefaultTCPListenAddr %q must be loopback (ADR-0003)", DefaultTCPListenAddr)
	}
}

func TestIsLoopbackListenAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:7777", true},
		{"localhost:7777", true},
		{"[::1]:7777", true},
		{"0.0.0.0:7777", false},
		{":7777", false},            // empty host binds every interface
		{"100.64.0.12:7777", false}, // specific non-loopback interface
		{"[::]:7777", false},
		{"garbage", false},
	}
	for _, tc := range cases {
		if got := isLoopbackListenAddr(tc.addr); got != tc.want {
			t.Errorf("isLoopbackListenAddr(%q) = %t, want %t", tc.addr, got, tc.want)
		}
	}
}
