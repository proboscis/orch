package cli

import (
	"strings"
	"testing"
)

func TestMonitorSessionNameForProject(t *testing.T) {
	t.Setenv("ZELLIJ_SOCKET_DIR", "/tmp/zellij-test")

	tests := []struct {
		projectPath string
		wantPrefix  string
	}{
		{"", defaultMonitorSessionName},
		{"/home/user/projects/myproject", "orch-myproject-"},
		{"/home/user/.project", "orch--project-"},
		{"/home/user/my project", "orch-my-project-"},
	}

	for _, tt := range tests {
		got := monitorSessionNameForProject(tt.projectPath)
		if !strings.HasPrefix(got, tt.wantPrefix) {
			t.Errorf("monitorSessionNameForProject(%q) = %q, want prefix %q", tt.projectPath, got, tt.wantPrefix)
		}
	}
}

func TestMonitorSessionNameFitsZellijSocketLimit(t *testing.T) {
	t.Setenv("ZELLIJ_SOCKET_DIR", "/var/folders/q2/8x7k2j9d5cl0abcd1234efgh5678/T/zellij-501")

	for _, projectPath := range []string{
		"/work/x",
		"/work/agent-control-plane",
		"/work/this-is-a-really-really-long-monorepo-name-here",
	} {
		name := monitorSessionNameForProject(projectPath)
		if socketPath := zellijSocketDir() + "/" + name; len(socketPath) >= zellijSockMaxLength {
			t.Errorf("session name %q yields overlong socket path %q", name, socketPath)
		}
	}
}
