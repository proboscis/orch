package daemon

import (
	"strings"
	"testing"

	"github.com/proboscis/orch/internal/config"
)

func newCodexProfileConfig() *config.Config {
	return &config.Config{
		Agent: "codex",
		Codex: config.CodexConfig{
			DefaultProfile: "company",
			Profiles: map[string]config.CodexProfile{
				"company": {
					Target:         "mac",
					CodexHome:      "~/.codex-company",
					AllowedTargets: []string{"mac"},
				},
				"personal": {},
			},
		},
		Targets: []config.TargetConfig{
			// Target NAME "mac" deliberately differs from its resolved host
			// "CA-20022388" so AllowedTargets is matched against target names,
			// not hostnames.
			{Name: "mac", Host: "CA-20022388"},
			{Name: "remotebox", Host: "remotebox"},
		},
	}
}

func TestApplyCodexProfile_DefaultProfileInjectsTargetAndCodexHome(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := newCodexProfileConfig()
	opts := &StartRunOptions{Agent: "codex"} // no explicit profile, no target

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.Target != "mac" {
		t.Errorf("opts.Target = %q, want mac (from default company profile)", opts.Target)
	}
	if opts.CodexHome != "~/.codex-company" {
		t.Errorf("opts.CodexHome = %q, want ~/.codex-company verbatim (execution host expands ~)", opts.CodexHome)
	}
}

func TestApplyCodexProfile_ExplicitProfileOverridesDefault(t *testing.T) {
	cfg := newCodexProfileConfig()
	opts := &StartRunOptions{Agent: "codex", CodexProfile: "personal"}

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	// personal has no target and no allowed_targets: stays local, no CODEX_HOME.
	if opts.Target != "" {
		t.Errorf("opts.Target = %q, want empty for personal profile", opts.Target)
	}
	if opts.CodexHome != "" {
		t.Errorf("opts.CodexHome = %q, want empty for personal profile", opts.CodexHome)
	}
}

func TestApplyCodexProfile_UnknownProfileFailsFast(t *testing.T) {
	cfg := newCodexProfileConfig()
	opts := &StartRunOptions{Agent: "codex", CodexProfile: "ghost"}

	err := applyCodexProfile(cfg, opts)
	if err == nil {
		t.Fatal("expected error for unknown codex profile, got nil")
	}
	if !strings.Contains(err.Error(), "unknown codex profile") || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error = %q, want it to name unknown codex profile %q", err.Error(), "ghost")
	}
}

func TestApplyCodexProfile_DisallowedTargetFailsFast(t *testing.T) {
	cfg := newCodexProfileConfig()
	// Company profile only allows mac, but caller pins target=remotebox.
	opts := &StartRunOptions{Agent: "codex", CodexProfile: "company", Target: "remotebox"}

	err := applyCodexProfile(cfg, opts)
	if err == nil {
		t.Fatal("expected fail-fast for company profile on disallowed target remotebox, got nil")
	}
	if !strings.Contains(err.Error(), "company") || !strings.Contains(err.Error(), "remotebox") {
		t.Errorf("error = %q, want it to name profile company and target remotebox", err.Error())
	}
}

// Regression guard: AllowedTargets compares target NAMES, not resolved hosts.
// The company profile allows target name "mac" (whose host is "CA-20022388").
// A run pinned to target name "mac" must pass; a run pinned to the raw host
// string "CA-20022388" (not a valid target name) must be rejected. This ensures
// the name-vs-host semantic mismatch cannot silently regress.
func TestApplyCodexProfile_AllowedTargetsAreNamesNotHosts(t *testing.T) {
	cfg := newCodexProfileConfig()

	// Target name matches -> allowed.
	allowed := &StartRunOptions{Agent: "codex", CodexProfile: "company", Target: "mac"}
	if err := applyCodexProfile(cfg, allowed); err != nil {
		t.Fatalf("target name mac should be allowed: %v", err)
	}

	// The resolved host string is NOT a target name and must be rejected.
	host := &StartRunOptions{Agent: "codex", CodexProfile: "company", Target: "CA-20022388"}
	err := applyCodexProfile(cfg, host)
	if err == nil {
		t.Fatal("expected fail-fast when Target is a hostname rather than the allowed target name, got nil")
	}
	if !strings.Contains(err.Error(), "CA-20022388") {
		t.Errorf("error = %q, want it to name the rejected value CA-20022388", err.Error())
	}
}

func TestApplyCodexProfile_ExplicitAllowedTargetPasses(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := newCodexProfileConfig()
	// Caller pins target=mac, which the company profile allows.
	opts := &StartRunOptions{Agent: "codex", CodexProfile: "company", Target: "mac"}

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.Target != "mac" {
		t.Errorf("opts.Target = %q, want mac (explicit target preserved)", opts.Target)
	}
	if opts.CodexHome != "~/.codex-company" {
		t.Errorf("opts.CodexHome = %q, want company codex home verbatim", opts.CodexHome)
	}
}

func TestApplyCodexProfile_NonCodexAgentIsNoOp(t *testing.T) {
	cfg := newCodexProfileConfig()
	cfg.Agent = "opencode"
	opts := &StartRunOptions{Agent: "opencode"}

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.Target != "" || opts.CodexHome != "" {
		t.Errorf("non-codex agent should be untouched, got Target=%q CodexHome=%q", opts.Target, opts.CodexHome)
	}
}

func TestApplyCodexProfile_NoProfileConfiguredIsNoOp(t *testing.T) {
	cfg := &config.Config{Agent: "codex"} // codex but no profiles / default_profile
	opts := &StartRunOptions{Agent: "codex"}

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.Target != "" || opts.CodexHome != "" {
		t.Errorf("no profile configured should be no-op, got Target=%q CodexHome=%q", opts.Target, opts.CodexHome)
	}
}

func TestApplyCodexProfile_LocalDisallowedWhenAllowedTargetsSet(t *testing.T) {
	// A profile that constrains allowed_targets but has no target must fail when
	// the run would execute locally (effective target is "local").
	cfg := &config.Config{
		Agent: "codex",
		Codex: config.CodexConfig{
			DefaultProfile: "company",
			Profiles: map[string]config.CodexProfile{
				"company": {AllowedTargets: []string{"mac"}},
			},
		},
	}
	opts := &StartRunOptions{Agent: "codex"}

	err := applyCodexProfile(cfg, opts)
	if err == nil {
		t.Fatal("expected fail-fast when allowed_targets excludes local execution, got nil")
	}
	if !strings.Contains(err.Error(), "local") {
		t.Errorf("error = %q, want it to mention local", err.Error())
	}
}

// Guard: the configured codex_home must reach opts VERBATIM — never expanded
// against the master's HOME. The same profile (~/.codex/profiles/personal)
// points at a different absolute path on each host, so expansion belongs to
// the execution host at launch (agent.LaunchConfig), and a master-side
// expansion would corrupt worker-delegated runs.
func TestApplyCodexProfile_CodexHomeNotExpandedOnMaster(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // a real HOME that must NOT leak into opts
	cfg := newCodexProfileConfig()
	opts := &StartRunOptions{Agent: "codex"}
	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.CodexHome != "~/.codex-company" {
		t.Errorf("opts.CodexHome = %q, want ~/.codex-company verbatim", opts.CodexHome)
	}
}

// --- restart-from / continue path ---

// A company run restarted-from must re-enforce the allowed-target constraint and
// re-derive CODEX_HOME, even though the continue path infers the agent from the
// prior run (opts.Agent is empty) and inherits the prior run's target.
func TestApplyCodexProfileContinue_RestartCompanyEnforcesAndCarriesCodexHome(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := newCodexProfileConfig()
	// Simulate restart-from: no explicit agent override, agent inferred from the
	// prior run (codex), and the prior run's target (mac) inherited.
	opts := &ContinueRunOptions{Target: "mac"}

	if err := applyCodexProfileContinue(cfg, opts, "codex", ""); err != nil {
		t.Fatalf("applyCodexProfileContinue error: %v", err)
	}
	if opts.Target != "mac" {
		t.Errorf("opts.Target = %q, want mac", opts.Target)
	}
	if opts.CodexHome != "~/.codex-company" {
		t.Errorf("opts.CodexHome = %q, want company codex home verbatim on restart", opts.CodexHome)
	}
}

// Restart-from must fail fast if the inherited/overridden target is disallowed
// for the profile (e.g. a company run somehow pointed at remotebox).
func TestApplyCodexProfileContinue_RestartDisallowedTargetFailsFast(t *testing.T) {
	cfg := newCodexProfileConfig()
	opts := &ContinueRunOptions{CodexProfile: "company", Target: "remotebox"}

	err := applyCodexProfileContinue(cfg, opts, "codex", "")
	if err == nil {
		t.Fatal("expected fail-fast restarting company run onto remotebox, got nil")
	}
	if !strings.Contains(err.Error(), "company") || !strings.Contains(err.Error(), "remotebox") {
		t.Errorf("error = %q, want it to name profile company and target remotebox", err.Error())
	}
}

// When the prior run used a non-codex agent, the continue path must be a no-op
// even though cfg has codex profiles configured.
func TestApplyCodexProfileContinue_NonCodexPriorAgentIsNoOp(t *testing.T) {
	cfg := newCodexProfileConfig()
	opts := &ContinueRunOptions{}

	if err := applyCodexProfileContinue(cfg, opts, "opencode", ""); err != nil {
		t.Fatalf("applyCodexProfileContinue error: %v", err)
	}
	if opts.Target != "" || opts.CodexHome != "" {
		t.Errorf("non-codex prior agent should be untouched, got Target=%q CodexHome=%q", opts.Target, opts.CodexHome)
	}
}

// Restart-from with default profile (no inherited target) must inject the
// profile's target so the run re-routes to the constrained host.
func TestApplyCodexProfileContinue_DefaultProfileInjectsTarget(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := newCodexProfileConfig()
	opts := &ContinueRunOptions{} // no target inherited

	if err := applyCodexProfileContinue(cfg, opts, "codex", ""); err != nil {
		t.Fatalf("applyCodexProfileContinue error: %v", err)
	}
	if opts.Target != "mac" {
		t.Errorf("opts.Target = %q, want mac (default company profile injected on restart)", opts.Target)
	}
}

func TestApplyCodexProfileContinue_InheritedProfileOverridesDefault(t *testing.T) {
	cfg := newCodexProfileConfig()
	opts := &ContinueRunOptions{}

	if err := applyCodexProfileContinue(cfg, opts, "codex", "personal"); err != nil {
		t.Fatalf("applyCodexProfileContinue error: %v", err)
	}
	if opts.CodexProfile != "personal" {
		t.Errorf("opts.CodexProfile = %q, want inherited personal profile", opts.CodexProfile)
	}
	if opts.Target != "" {
		t.Errorf("opts.Target = %q, want empty: inherited personal profile has no target", opts.Target)
	}
	if opts.CodexHome != "" {
		t.Errorf("opts.CodexHome = %q, want empty: inherited personal profile uses agent default", opts.CodexHome)
	}
}

func TestApplyCodexProfileContinue_ExplicitProfileOverridesInherited(t *testing.T) {
	cfg := newCodexProfileConfig()
	opts := &ContinueRunOptions{CodexProfile: "company", Target: "mac"}

	if err := applyCodexProfileContinue(cfg, opts, "codex", "personal"); err != nil {
		t.Fatalf("applyCodexProfileContinue error: %v", err)
	}
	if opts.CodexProfile != "company" {
		t.Errorf("opts.CodexProfile = %q, want explicit company profile", opts.CodexProfile)
	}
}

func TestApplyCodexProfileContinue_UnknownInheritedProfileFailsFast(t *testing.T) {
	cfg := newCodexProfileConfig()
	opts := &ContinueRunOptions{}

	err := applyCodexProfileContinue(cfg, opts, "codex", "ghost")
	if err == nil {
		t.Fatal("expected fail-fast for unknown inherited codex profile, got nil")
	}
	if !strings.Contains(err.Error(), "unknown codex profile") || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error = %q, want it to name inherited unknown profile ghost", err.Error())
	}
}

// --- control agent path ---

// setDaemonHostname stubs the daemon's hostname for the test.
func setDaemonHostname(t *testing.T, host string) {
	t.Helper()
	origHost := currentDaemonHostname
	currentDaemonHostname = func() (string, error) { return host, nil }
	t.Cleanup(func() { currentDaemonHostname = origHost })
}

// The control agent executes on the CLIENT host. A client on an allowed
// target (mac, host CA-20022388) must be ALLOWED even when the DAEMON runs on
// a disallowed host (remotebox) — regression for the reported false denial
// (mac client against the zeus master).
func TestResolveControlCodexHome_ClientOnAllowedTargetDaemonElsewhereIsAllowed(t *testing.T) {
	cfg := newCodexProfileConfig() // company profile: allowed_targets [mac]
	setDaemonHostname(t, "remotebox")

	home, err := resolveControlCodexHome(cfg, "codex", "CA-20022388")
	if err != nil {
		t.Fatalf("resolveControlCodexHome error: %v (client host is the allowed mac target; the daemon host must not be enforced)", err)
	}
	if home != "~/.codex-company" {
		t.Errorf("control CODEX_HOME = %q, want ~/.codex-company VERBATIM (client expands ~)", home)
	}
}

// A client on a disallowed host must be DENIED even when the DAEMON runs on
// the allowed target — closes the policy hole where the company account could
// launch on the personal machine because only the daemon host was checked.
func TestResolveControlCodexHome_ClientOnDisallowedHostDaemonAllowedIsDenied(t *testing.T) {
	cfg := newCodexProfileConfig()
	setDaemonHostname(t, "CA-20022388") // daemon on the allowed mac target

	home, err := resolveControlCodexHome(cfg, "codex", "remotebox")
	if err == nil {
		t.Fatal("expected fail-fast for company control agent on client host remotebox, got nil")
	}
	if home != "" {
		t.Errorf("CODEX_HOME = %q, want empty on denial (must not hand back company home)", home)
	}
	for _, want := range []string{"company", "remotebox", "[mac]", "may only run on targets"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// A client host that matches no configured target maps to "local" and is
// DENIED (fail-closed); the error names the client host and allowed targets.
func TestResolveControlCodexHome_UnmappedClientHostFailsClosed(t *testing.T) {
	cfg := newCodexProfileConfig()
	setDaemonHostname(t, "CA-20022388")

	_, err := resolveControlCodexHome(cfg, "codex", "some-other-laptop")
	if err == nil {
		t.Fatal("expected fail-fast when client host maps to no target, got nil")
	}
	for _, want := range []string{"some-other-laptop", `"local"`, "[mac]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

// Empty client_host (old client) falls back to enforcing against the DAEMON
// host (back-compat): allowed when the daemon is on the allowed target...
func TestResolveControlCodexHome_EmptyClientHostUsesDaemonHost(t *testing.T) {
	cfg := newCodexProfileConfig()
	setDaemonHostname(t, "CA-20022388")

	home, err := resolveControlCodexHome(cfg, "codex", "")
	if err != nil {
		t.Fatalf("resolveControlCodexHome error: %v (daemon host is the allowed mac target)", err)
	}
	if home != "~/.codex-company" {
		t.Errorf("control CODEX_HOME = %q, want ~/.codex-company verbatim", home)
	}
}

// ...and denied when the daemon is on a disallowed host.
func TestResolveControlCodexHome_EmptyClientHostDaemonDisallowedIsDenied(t *testing.T) {
	cfg := newCodexProfileConfig()
	setDaemonHostname(t, "remotebox")

	home, err := resolveControlCodexHome(cfg, "codex", "")
	if err == nil {
		t.Fatal("expected fail-fast for company control agent with daemon on remotebox, got nil")
	}
	if !strings.Contains(err.Error(), "company") || !strings.Contains(err.Error(), "remotebox") {
		t.Errorf("error = %q, want it to name profile company and host remotebox", err.Error())
	}
	if home != "" {
		t.Errorf("CODEX_HOME = %q, want empty on disallowed host (must not hand back company home)", home)
	}
}

// Client hostnames match targets with the same short-hostname/case-insensitive
// semantics as isLocalExecutionHost (e.g. mDNS-style "CA-20022388.local").
func TestResolveControlCodexHome_ClientHostShortNameMatch(t *testing.T) {
	cfg := newCodexProfileConfig()
	setDaemonHostname(t, "remotebox")

	home, err := resolveControlCodexHome(cfg, "codex", "ca-20022388.local")
	if err != nil {
		t.Fatalf("resolveControlCodexHome error: %v (short-name match must resolve to mac)", err)
	}
	if home != "~/.codex-company" {
		t.Errorf("control CODEX_HOME = %q, want ~/.codex-company", home)
	}
}

// codex_home is returned VERBATIM: never expanded against the daemon's HOME
// (a remote master would bake a daemon-shaped path into the client env).
func TestResolveControlCodexHome_CodexHomeVerbatim(t *testing.T) {
	t.Setenv("HOME", "/home/daemon-side")
	cfg := newCodexProfileConfig()
	setDaemonHostname(t, "remotebox")

	home, err := resolveControlCodexHome(cfg, "codex", "CA-20022388")
	if err != nil {
		t.Fatalf("resolveControlCodexHome error: %v", err)
	}
	if home != "~/.codex-company" {
		t.Errorf("control CODEX_HOME = %q, want leading ~ preserved (no daemon-side expansion)", home)
	}
}

func TestResolveControlCodexHome_NonCodexIsNoOp(t *testing.T) {
	cfg := newCodexProfileConfig()
	home, err := resolveControlCodexHome(cfg, "opencode", "")
	if err != nil {
		t.Fatalf("resolveControlCodexHome error: %v", err)
	}
	if home != "" {
		t.Errorf("non-codex control agent CODEX_HOME = %q, want empty", home)
	}
}

func TestResolveControlCodexHome_UnknownDefaultProfileFailsFast(t *testing.T) {
	cfg := &config.Config{
		Agent: "codex",
		Codex: config.CodexConfig{DefaultProfile: "ghost"},
	}
	_, err := resolveControlCodexHome(cfg, "codex", "")
	if err == nil {
		t.Fatal("expected fail-fast for unknown default control profile, got nil")
	}
	if !strings.Contains(err.Error(), "unknown codex profile") {
		t.Errorf("error = %q, want unknown codex profile", err.Error())
	}
}

// localTargetName is the daemon-host specialization of targetNameForHost.
func TestTargetNameForHost_DaemonHostEqualsLocalTargetName(t *testing.T) {
	cfg := newCodexProfileConfig()
	for _, host := range []string{"CA-20022388", "remotebox", "unmapped-host"} {
		setDaemonHostname(t, host)
		if got, want := localTargetName(cfg), targetNameForHost(cfg, host); got != want {
			t.Errorf("localTargetName = %q, targetNameForHost(%q) = %q, want equal", got, host, want)
		}
	}
}

// Loopback target hosts designate the daemon's machine: they match the daemon
// hostname but never a different client host.
func TestTargetNameForHost_LoopbackTargetMatchesDaemonHostOnly(t *testing.T) {
	cfg := &config.Config{
		Targets: []config.TargetConfig{{Name: "master", Host: "localhost"}},
	}
	setDaemonHostname(t, "zeus")

	if got := targetNameForHost(cfg, "zeus"); got != "master" {
		t.Errorf("targetNameForHost(daemon host) = %q, want master (loopback target designates the daemon machine)", got)
	}
	if got := targetNameForHost(cfg, "CA-20022388"); got != "local" {
		t.Errorf("targetNameForHost(other client) = %q, want local (loopback must not match a remote client)", got)
	}
}

func TestApplyCodexProfile_WritesBackResolvedDefaultProfileName(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := newCodexProfileConfig()
	opts := &StartRunOptions{Agent: "codex"} // no explicit profile -> default "company"

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.CodexProfile != "company" {
		t.Errorf("opts.CodexProfile = %q, want company (resolved default written back)", opts.CodexProfile)
	}
}

func TestApplyCodexProfile_NonCodexAgentClearsRequestedProfile(t *testing.T) {
	cfg := newCodexProfileConfig()
	cfg.Agent = "opencode"
	opts := &StartRunOptions{Agent: "opencode", CodexProfile: "company"}

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.CodexProfile != "" {
		t.Errorf("opts.CodexProfile = %q, want empty: a codex profile does not apply to a non-codex agent", opts.CodexProfile)
	}
}

func TestApplyCodexProfileContinue_WritesBackResolvedProfileName(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := newCodexProfileConfig()
	opts := &ContinueRunOptions{Target: "mac"} // agent inferred from prior run

	if err := applyCodexProfileContinue(cfg, opts, "codex", ""); err != nil {
		t.Fatalf("applyCodexProfileContinue error: %v", err)
	}
	if opts.CodexProfile != "company" {
		t.Errorf("opts.CodexProfile = %q, want company (resolved default written back)", opts.CodexProfile)
	}
}

func TestEffectiveAgentProfile_CodexProfileWins(t *testing.T) {
	if got := effectiveAgentProfile("company", "my-claude-profile"); got != "company" {
		t.Errorf("effectiveAgentProfile = %q, want company", got)
	}
	if got := effectiveAgentProfile("", "my-claude-profile"); got != "my-claude-profile" {
		t.Errorf("effectiveAgentProfile = %q, want my-claude-profile", got)
	}
	if got := effectiveAgentProfile("  ", ""); got != "" {
		t.Errorf("effectiveAgentProfile = %q, want empty", got)
	}
}

func TestApplyCodexProfile_NonCodexAgentPreservesIncomingTarget(t *testing.T) {
	// Regression: the no-op paths of resolveCodexProfile must not drop the
	// caller's --on target (it was cleared for non-codex agents, masked by the
	// master projection reading the raw request target).
	cfg := newCodexProfileConfig()
	cfg.Agent = "opencode"
	opts := &StartRunOptions{Agent: "custom", Target: "remotebox"}

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.Target != "remotebox" {
		t.Errorf("opts.Target = %q, want remotebox (caller target preserved for non-codex agent)", opts.Target)
	}
}

func TestApplyCodexProfile_CodexWithoutProfilePreservesIncomingTarget(t *testing.T) {
	cfg := &config.Config{Agent: "codex"} // codex but no profiles / default_profile
	opts := &StartRunOptions{Agent: "codex", Target: "remotebox"}

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.Target != "remotebox" {
		t.Errorf("opts.Target = %q, want remotebox (caller target preserved without profile)", opts.Target)
	}
}

func TestApplyCodexProfileContinue_NonCodexAgentPreservesIncomingTarget(t *testing.T) {
	cfg := newCodexProfileConfig()
	opts := &ContinueRunOptions{Target: "remotebox"}

	if err := applyCodexProfileContinue(cfg, opts, "claude", ""); err != nil {
		t.Fatalf("applyCodexProfileContinue error: %v", err)
	}
	if opts.Target != "remotebox" {
		t.Errorf("opts.Target = %q, want remotebox (prior run target preserved for non-codex agent)", opts.Target)
	}
}

func TestApplyCodexProfile_ExplicitLocalTargetNormalizedToEmpty(t *testing.T) {
	cfg := newCodexProfileConfig()
	cfg.Agent = "opencode"
	opts := &StartRunOptions{Agent: "opencode", Target: "local"}

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.Target != "" {
		t.Errorf("opts.Target = %q, want empty (explicit local normalized)", opts.Target)
	}
}

// --- claude execution profiles ---

func newClaudeProfileConfig() *config.Config {
	return &config.Config{
		Agent: "claude",
		Claude: config.ClaudeConfig{
			DefaultProfile: "corp",
			Profiles: map[string]config.ClaudeProfile{
				"corp": {
					Target:         "mac",
					AllowedTargets: []string{"mac"},
					// no config_dir: ca lives at the agent default (~/.claude)
				},
				"work": {
					ConfigDir: "~/.config/claude-work",
				},
				"personal": {
					ConfigDir: "~/.config/claude-personal",
				},
			},
		},
		Targets: []config.TargetConfig{
			{Name: "mac", Host: "CA-20022388"},
			{Name: "remotebox", Host: "remotebox"},
		},
	}
}

func TestApplyClaudeProfile_DefaultProfileInjectsTarget(t *testing.T) {
	cfg := newClaudeProfileConfig()
	opts := &StartRunOptions{Agent: "claude"} // no explicit profile, no target

	if err := applyClaudeProfile(cfg, opts); err != nil {
		t.Fatalf("applyClaudeProfile error: %v", err)
	}
	if opts.AgentProfile != "corp" {
		t.Errorf("opts.AgentProfile = %q, want ca (default profile written back)", opts.AgentProfile)
	}
	if opts.Target != "mac" {
		t.Errorf("opts.Target = %q, want mac (from default ca profile)", opts.Target)
	}
	if opts.ClaudeConfigDir != "" {
		t.Errorf("opts.ClaudeConfigDir = %q, want empty (ca uses the agent default dir)", opts.ClaudeConfigDir)
	}
}

func TestApplyClaudeProfile_ExplicitProfileCarriesConfigDirVerbatim(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // must NOT leak into the config dir
	cfg := newClaudeProfileConfig()
	opts := &StartRunOptions{Agent: "claude", AgentProfile: "work", Target: "remotebox"}

	if err := applyClaudeProfile(cfg, opts); err != nil {
		t.Fatalf("applyClaudeProfile error: %v", err)
	}
	if opts.ClaudeConfigDir != "~/.config/claude-work" {
		t.Errorf("opts.ClaudeConfigDir = %q, want ~/.config/claude-work verbatim (execution host expands ~)", opts.ClaudeConfigDir)
	}
	if opts.Target != "remotebox" {
		t.Errorf("opts.Target = %q, want remotebox (unconstrained profile keeps caller target)", opts.Target)
	}
}

func TestApplyClaudeProfile_DisallowedTargetFailsFast(t *testing.T) {
	cfg := newClaudeProfileConfig()
	opts := &StartRunOptions{Agent: "claude", AgentProfile: "corp", Target: "remotebox"}

	err := applyClaudeProfile(cfg, opts)
	if err == nil {
		t.Fatal("expected fail-fast launching corp profile on remotebox, got nil")
	}
	if !strings.Contains(err.Error(), "remotebox") {
		t.Errorf("error = %q, want it to mention the disallowed target", err.Error())
	}
}

func TestApplyClaudeProfile_UnknownProfileFailsFast(t *testing.T) {
	cfg := newClaudeProfileConfig()
	opts := &StartRunOptions{Agent: "claude", AgentProfile: "ghost"}

	if err := applyClaudeProfile(cfg, opts); err == nil {
		t.Fatal("expected fail-fast for unknown claude profile, got nil")
	}
}

func TestApplyClaudeProfile_NonClaudeAgentIsUntouched(t *testing.T) {
	cfg := newClaudeProfileConfig()
	cfg.Agent = "codex"
	opts := &StartRunOptions{Agent: "codex", AgentProfile: "anything", Target: "remotebox"}

	if err := applyClaudeProfile(cfg, opts); err != nil {
		t.Fatalf("applyClaudeProfile error: %v", err)
	}
	if opts.AgentProfile != "anything" || opts.Target != "remotebox" || opts.ClaudeConfigDir != "" {
		t.Errorf("non-claude agent must be untouched, got AgentProfile=%q Target=%q ClaudeConfigDir=%q", opts.AgentProfile, opts.Target, opts.ClaudeConfigDir)
	}
}

func TestApplyClaudeProfile_NoProfilesConfiguredIsNoOp(t *testing.T) {
	cfg := &config.Config{Agent: "claude"} // no claude.profiles / default_profile
	opts := &StartRunOptions{Agent: "claude"}

	if err := applyClaudeProfile(cfg, opts); err != nil {
		t.Fatalf("applyClaudeProfile error: %v", err)
	}
	if opts.AgentProfile != "" || opts.ClaudeConfigDir != "" {
		t.Errorf("no profiles configured should be no-op, got AgentProfile=%q ClaudeConfigDir=%q", opts.AgentProfile, opts.ClaudeConfigDir)
	}
}

// Restart-from must re-enforce the claude profile constraint and re-derive
// CLAUDE_CONFIG_DIR even though the continue path infers the agent from the
// prior run (opts.Agent is empty).
func TestApplyClaudeProfileContinue_RestartCarriesConfigDir(t *testing.T) {
	cfg := newClaudeProfileConfig()
	opts := &ContinueRunOptions{AgentProfile: "personal", Target: "remotebox"}

	if err := applyClaudeProfileContinue(cfg, opts, "claude", ""); err != nil {
		t.Fatalf("applyClaudeProfileContinue error: %v", err)
	}
	if opts.ClaudeConfigDir != "~/.config/claude-personal" {
		t.Errorf("opts.ClaudeConfigDir = %q, want personal config dir verbatim on restart", opts.ClaudeConfigDir)
	}
}

func TestApplyClaudeProfileContinue_InheritedProfileOverridesDefault(t *testing.T) {
	cfg := newClaudeProfileConfig()
	opts := &ContinueRunOptions{}

	if err := applyClaudeProfileContinue(cfg, opts, "claude", "personal"); err != nil {
		t.Fatalf("applyClaudeProfileContinue error: %v", err)
	}
	if opts.AgentProfile != "personal" {
		t.Errorf("opts.AgentProfile = %q, want inherited personal profile", opts.AgentProfile)
	}
	if opts.ClaudeConfigDir != "~/.config/claude-personal" {
		t.Errorf("opts.ClaudeConfigDir = %q, want inherited personal config dir", opts.ClaudeConfigDir)
	}
}

func TestApplyClaudeProfileContinue_RestartDisallowedTargetFailsFast(t *testing.T) {
	cfg := newClaudeProfileConfig()
	opts := &ContinueRunOptions{AgentProfile: "corp", Target: "remotebox"}

	if err := applyClaudeProfileContinue(cfg, opts, "claude", ""); err == nil {
		t.Fatal("expected fail-fast restarting corp claude run onto remotebox, got nil")
	}
}
