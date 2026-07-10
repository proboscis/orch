package daemon

// Proto-level tests for the control-agent client-host policy: the control
// agent executes on the CLIENT host, so both control-agent RPCs carry
// client_host and the daemon enforces the codex profile's allowed_targets
// against it (falling back to the daemon host for old clients).

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/proboscis/orch/api/orchpb"
	"github.com/proboscis/orch/internal/model"
)

// newControlAgentPolicyServer starts a socket server whose project config has
// a codex control agent with the company profile locked to the "mac" target
// (host CA-20035844); a second "zeus" target exists but is not allowed.
func newControlAgentPolicyServer(t *testing.T) *SocketServer {
	t.Helper()
	cleanup := setupXDGTestEnv(t)
	t.Cleanup(cleanup)

	projectRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".orch"), 0o755); err != nil {
		t.Fatalf("mkdir .orch: %v", err)
	}
	configYAML := []byte(`agent: codex
control_agent: codex
codex:
  default_profile: company
  profiles:
    company:
      target: mac
      codex_home: "~/.codex/profiles/company"
      allowed_targets: [mac]
targets:
  - name: mac
    host: CA-20035844
  - name: zeus
    host: zeus
`)
	if err := os.WriteFile(filepath.Join(projectRoot, ".orch", "config.yaml"), configYAML, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	st := &mockStore{runs: map[string]*model.Run{}, issues: map[string]*model.Issue{}}
	logger := log.New(io.Discard, "", 0)
	server := NewSocketServer(nil, logger)
	registerRepoContextForTest(t, server, "project-ctx", projectRoot, st)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(server.Stop)
	return server
}

func controlAgentConfigRequest(clientHost string) *orchpb.Request {
	return &orchpb.Request{
		Request: &orchpb.Request_GetControlAgentConfig{
			GetControlAgentConfig: &orchpb.GetControlAgentConfigRequest{
				Context:    &orchpb.RequestContext{ProjectId: "project-ctx"},
				ClientHost: clientHost,
			},
		},
	}
}

// The reported false denial: client on the allowed mac target, daemon on
// zeus. The RPC must succeed and return the codex_home VERBATIM.
func TestProtoGetControlAgentConfig_ClientHostOnAllowedTargetIsAllowed(t *testing.T) {
	newControlAgentPolicyServer(t)
	setDaemonHostname(t, "zeus")

	resp := sendProtoRequest(t, controlAgentConfigRequest("CA-20035844"))
	if !resp.Ok {
		t.Fatalf("expected ok response for client on allowed target, got error: %s", resp.Error)
	}
	cfgResp := resp.GetGetControlAgentConfig()
	if cfgResp == nil {
		t.Fatal("expected GetControlAgentConfig response payload")
	}
	if cfgResp.CodexHome != "~/.codex/profiles/company" {
		t.Fatalf("codex_home = %q, want ~/.codex/profiles/company VERBATIM (client expands ~)", cfgResp.CodexHome)
	}
}

// The policy hole: client on zeus (disallowed), daemon on the allowed mac
// target. The RPC must be DENIED and the error must name the client host,
// the resolved target, and the allowed targets.
func TestProtoGetControlAgentConfig_ClientHostOnDisallowedTargetIsDenied(t *testing.T) {
	newControlAgentPolicyServer(t)
	setDaemonHostname(t, "CA-20035844")

	resp := sendProtoRequest(t, controlAgentConfigRequest("zeus"))
	if resp.Ok {
		t.Fatal("expected policy denial for client on disallowed host, got ok")
	}
	for _, want := range []string{"may only run on targets", "[mac]", `"zeus"`} {
		if !strings.Contains(resp.Error, want) {
			t.Errorf("error = %q, want it to contain %q", resp.Error, want)
		}
	}
}

// Back-compat: an old client that sends no client_host is enforced against
// the daemon host, exactly as before.
func TestProtoGetControlAgentConfig_EmptyClientHostEnforcesDaemonHost(t *testing.T) {
	newControlAgentPolicyServer(t)
	setDaemonHostname(t, "zeus")

	resp := sendProtoRequest(t, controlAgentConfigRequest(""))
	if resp.Ok {
		t.Fatal("expected denial: empty client_host must fall back to the (disallowed) daemon host")
	}
	if !strings.Contains(resp.Error, "zeus") {
		t.Errorf("error = %q, want it to name the daemon host zeus", resp.Error)
	}

	setDaemonHostname(t, "CA-20035844")
	resp = sendProtoRequest(t, controlAgentConfigRequest(""))
	if !resp.Ok {
		t.Fatalf("expected ok: empty client_host with daemon on the allowed target, got error: %s", resp.Error)
	}
}

// The launch RPC routes through the same decision point: client_host must be
// plumbed through and enforced (denial happens before any adapter/CLI check,
// so this holds regardless of codex availability on the test host).
func TestProtoGetControlAgentLaunch_ClientHostOnDisallowedTargetIsDenied(t *testing.T) {
	newControlAgentPolicyServer(t)
	setDaemonHostname(t, "CA-20035844")

	resp := sendProtoRequest(t, &orchpb.Request{
		Request: &orchpb.Request_GetControlAgentLaunch{
			GetControlAgentLaunch: &orchpb.GetControlAgentLaunchRequest{
				Context:    &orchpb.RequestContext{ProjectId: "project-ctx"},
				ClientHost: "zeus",
			},
		},
	})
	if resp.Ok {
		t.Fatal("expected policy denial for launch RPC with client on disallowed host, got ok")
	}
	for _, want := range []string{"may only run on targets", "[mac]", `"zeus"`} {
		if !strings.Contains(resp.Error, want) {
			t.Errorf("error = %q, want it to contain %q", resp.Error, want)
		}
	}
}
