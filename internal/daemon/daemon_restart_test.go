package daemon

import (
	"reflect"
	"testing"
)

func TestDaemonRunArgsPreserveExplicitlyDisabledTCPListener(t *testing.T) {
	want := []string{"/tmp/orch", "daemon", "run", "--listen", ""}
	if got := daemonRunArgs("/tmp/orch", ""); !reflect.DeepEqual(got, want) {
		t.Fatalf("daemonRunArgs() = %#v, want %#v", got, want)
	}
}

func TestDaemonRunArgsPreserveListenAddress(t *testing.T) {
	want := []string{"/tmp/orch", "daemon", "run", "--listen", "tcp://0.0.0.0:7777"}
	if got := daemonRunArgs("/tmp/orch", " tcp://0.0.0.0:7777 "); !reflect.DeepEqual(got, want) {
		t.Fatalf("daemonRunArgs() = %#v, want %#v", got, want)
	}
}
