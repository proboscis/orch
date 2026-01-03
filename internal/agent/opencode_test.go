package agent

import (
	"strings"
	"testing"
)

func TestOpenCodeLaunchCommand(t *testing.T) {
	adapter := &OpenCodeAdapter{}

	tests := []struct {
		name       string
		cfg        *LaunchConfig
		wantSuffix string
	}{
		{
			name:       "default port",
			cfg:        &LaunchConfig{},
			wantSuffix: "opencode serve --port 4096 --hostname 0.0.0.0",
		},
		{
			name:       "custom port",
			cfg:        &LaunchConfig{Port: 5000},
			wantSuffix: "opencode serve --port 5000 --hostname 0.0.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := adapter.LaunchCommand(tt.cfg)
			if err != nil {
				t.Fatalf("LaunchCommand error: %v", err)
			}
			if !strings.HasSuffix(cmd, tt.wantSuffix) {
				t.Fatalf("command = %q, want suffix %q", cmd, tt.wantSuffix)
			}
		})
	}
}

func TestOpenCodeType(t *testing.T) {
	adapter := &OpenCodeAdapter{}
	if adapter.Type() != AgentOpenCode {
		t.Fatalf("Type() = %v, want %v", adapter.Type(), AgentOpenCode)
	}
}

func TestOpenCodePromptInjection(t *testing.T) {
	adapter := &OpenCodeAdapter{}
	if adapter.PromptInjection() != InjectionHTTP {
		t.Fatalf("PromptInjection() = %v, want %v", adapter.PromptInjection(), InjectionHTTP)
	}
}

func TestOpenCodeAttachCommand(t *testing.T) {
	adapter := &OpenCodeAdapter{}
	cmd := adapter.AttachCommand(5000)
	want := "opencode attach http://127.0.0.1:5000"
	if cmd != want {
		t.Fatalf("AttachCommand() = %q, want %q", cmd, want)
	}
}

func TestOpenCodeHealthEndpoint(t *testing.T) {
	adapter := &OpenCodeAdapter{}
	url := adapter.HealthEndpoint(5000)
	want := "http://127.0.0.1:5000/global/health"
	if url != want {
		t.Fatalf("HealthEndpoint() = %q, want %q", url, want)
	}
}

func TestOpenCodeEnv(t *testing.T) {
	adapter := &OpenCodeAdapter{}
	env := adapter.Env()
	if len(env) != 1 {
		t.Fatalf("Env() returned %d items, want 1", len(env))
	}
	// Check that the permission env var is set
	if env[0] == "" {
		t.Fatal("Env()[0] is empty")
	}
}

func TestParseAgentTypeOpenCode(t *testing.T) {
	agentType, err := ParseAgentType("opencode")
	if err != nil {
		t.Fatalf("ParseAgentType error: %v", err)
	}
	if agentType != AgentOpenCode {
		t.Fatalf("agentType = %v, want %v", agentType, AgentOpenCode)
	}
}

func TestGetAdapterOpenCode(t *testing.T) {
	adapter, err := GetAdapter(AgentOpenCode)
	if err != nil {
		t.Fatalf("GetAdapter error: %v", err)
	}
	if _, ok := adapter.(*OpenCodeAdapter); !ok {
		t.Fatalf("adapter is not *OpenCodeAdapter")
	}
}
