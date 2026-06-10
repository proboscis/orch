package daemon

import (
	"strings"
	"testing"

	"github.com/s22625/orch/internal/config"
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
			{Name: "zeus", Host: "zeus"},
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
	// Company profile only allows mac, but caller pins target=zeus.
	opts := &StartRunOptions{Agent: "codex", CodexProfile: "company", Target: "zeus"}

	err := applyCodexProfile(cfg, opts)
	if err == nil {
		t.Fatal("expected fail-fast for company profile on disallowed target zeus, got nil")
	}
	if !strings.Contains(err.Error(), "company") || !strings.Contains(err.Error(), "zeus") {
		t.Errorf("error = %q, want it to name profile company and target zeus", err.Error())
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

	if err := applyCodexProfileContinue(cfg, opts, "codex"); err != nil {
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
// for the profile (e.g. a company run somehow pointed at zeus).
func TestApplyCodexProfileContinue_RestartDisallowedTargetFailsFast(t *testing.T) {
	cfg := newCodexProfileConfig()
	opts := &ContinueRunOptions{CodexProfile: "company", Target: "zeus"}

	err := applyCodexProfileContinue(cfg, opts, "codex")
	if err == nil {
		t.Fatal("expected fail-fast restarting company run onto zeus, got nil")
	}
	if !strings.Contains(err.Error(), "company") || !strings.Contains(err.Error(), "zeus") {
		t.Errorf("error = %q, want it to name profile company and target zeus", err.Error())
	}
}

// When the prior run used a non-codex agent, the continue path must be a no-op
// even though cfg has codex profiles configured.
func TestApplyCodexProfileContinue_NonCodexPriorAgentIsNoOp(t *testing.T) {
	cfg := newCodexProfileConfig()
	opts := &ContinueRunOptions{}

	if err := applyCodexProfileContinue(cfg, opts, "opencode"); err != nil {
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

	if err := applyCodexProfileContinue(cfg, opts, "codex"); err != nil {
		t.Fatalf("applyCodexProfileContinue error: %v", err)
	}
	if opts.Target != "mac" {
		t.Errorf("opts.Target = %q, want mac (default company profile injected on restart)", opts.Target)
	}
}

// --- control agent path ---

// The control agent runs locally and MUST enforce AllowedTargets against the
// local daemon host: on an allowed host (mac) it proceeds and returns the
// company CODEX_HOME.
func TestResolveControlCodexHome_AllowedLocalHostSetsCodexHome(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := newCodexProfileConfig() // company profile: target mac, allowed_targets [mac]

	// Local daemon host resolves to the "mac" target (host CA-20022388).
	origHost := currentDaemonHostname
	currentDaemonHostname = func() (string, error) { return "CA-20022388", nil }
	t.Cleanup(func() { currentDaemonHostname = origHost })

	home, err := resolveControlCodexHome(cfg, "codex")
	if err != nil {
		t.Fatalf("resolveControlCodexHome error: %v", err)
	}
	if home != "/home/tester/.codex-company" {
		t.Errorf("control CODEX_HOME = %q, want /home/tester/.codex-company on allowed host", home)
	}
}

// On a disallowed local host (zeus), a company control agent must FAIL FAST and
// return NO CODEX_HOME (the company account must not launch on zeus).
func TestResolveControlCodexHome_DisallowedLocalHostFailsFast(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := newCodexProfileConfig() // company profile: target mac, allowed_targets [mac]

	// Local daemon host resolves to the "zeus" target (host zeus).
	origHost := currentDaemonHostname
	currentDaemonHostname = func() (string, error) { return "zeus", nil }
	t.Cleanup(func() { currentDaemonHostname = origHost })

	home, err := resolveControlCodexHome(cfg, "codex")
	if err == nil {
		t.Fatal("expected fail-fast for company control agent on zeus, got nil")
	}
	if !strings.Contains(err.Error(), "company") || !strings.Contains(err.Error(), "zeus") {
		t.Errorf("error = %q, want it to name profile company and target zeus", err.Error())
	}
	if home != "" {
		t.Errorf("CODEX_HOME = %q, want empty on disallowed host (must not hand back company home)", home)
	}
}

// When the local host matches no configured target, it maps to "local"; a
// company profile that only allows mac must reject the bare local host too.
func TestResolveControlCodexHome_UnmatchedLocalHostMapsToLocalAndFailsFast(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	cfg := newCodexProfileConfig()

	origHost := currentDaemonHostname
	currentDaemonHostname = func() (string, error) { return "some-other-laptop", nil }
	t.Cleanup(func() { currentDaemonHostname = origHost })

	_, err := resolveControlCodexHome(cfg, "codex")
	if err == nil {
		t.Fatal("expected fail-fast when local host maps to 'local' and is not allowed, got nil")
	}
	if !strings.Contains(err.Error(), "local") {
		t.Errorf("error = %q, want it to mention local", err.Error())
	}
}

func TestResolveControlCodexHome_NonCodexIsNoOp(t *testing.T) {
	cfg := newCodexProfileConfig()
	home, err := resolveControlCodexHome(cfg, "opencode")
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
	_, err := resolveControlCodexHome(cfg, "codex")
	if err == nil {
		t.Fatal("expected fail-fast for unknown default control profile, got nil")
	}
	if !strings.Contains(err.Error(), "unknown codex profile") {
		t.Errorf("error = %q, want unknown codex profile", err.Error())
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

	if err := applyCodexProfileContinue(cfg, opts, "codex"); err != nil {
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
	opts := &StartRunOptions{Agent: "custom", Target: "zeus"}

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.Target != "zeus" {
		t.Errorf("opts.Target = %q, want zeus (caller target preserved for non-codex agent)", opts.Target)
	}
}

func TestApplyCodexProfile_CodexWithoutProfilePreservesIncomingTarget(t *testing.T) {
	cfg := &config.Config{Agent: "codex"} // codex but no profiles / default_profile
	opts := &StartRunOptions{Agent: "codex", Target: "zeus"}

	if err := applyCodexProfile(cfg, opts); err != nil {
		t.Fatalf("applyCodexProfile error: %v", err)
	}
	if opts.Target != "zeus" {
		t.Errorf("opts.Target = %q, want zeus (caller target preserved without profile)", opts.Target)
	}
}

func TestApplyCodexProfileContinue_NonCodexAgentPreservesIncomingTarget(t *testing.T) {
	cfg := newCodexProfileConfig()
	opts := &ContinueRunOptions{Target: "zeus"}

	if err := applyCodexProfileContinue(cfg, opts, "claude"); err != nil {
		t.Fatalf("applyCodexProfileContinue error: %v", err)
	}
	if opts.Target != "zeus" {
		t.Errorf("opts.Target = %q, want zeus (prior run target preserved for non-codex agent)", opts.Target)
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
