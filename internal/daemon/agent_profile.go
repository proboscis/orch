package daemon

import (
	"fmt"
	"strings"

	"github.com/proboscis/orch/internal/config"
)

// agentProfileDecision is the resolved outcome of selecting an execution
// profile for an agent (codex or claude).
type agentProfileDecision struct {
	// ProfileName is the resolved profile name (explicit request or the
	// per-agent default_profile fallback). Empty means no profile applies.
	ProfileName string
	// Target is the (possibly profile-injected) config.targets name the run
	// should execute on. Empty means master/local execution.
	Target string
	// AuthDir is the profile's auth directory (CODEX_HOME for codex,
	// CLAUDE_CONFIG_DIR for claude), VERBATIM as configured: a leading ~ is
	// expanded on the execution host at launch (agent.LaunchConfig), never
	// on the master, so the same profile works across hosts with different
	// HOMEs. Empty means the agent default (~/.codex / ~/.claude).
	AuthDir string
}

// executionProfileBinding is the agent-independent shape of an execution
// profile: which target it runs on, which targets it may run on, and which
// auth directory the agent process gets.
type executionProfileBinding struct {
	Target         string
	AllowedTargets []string
	AuthDir        string
}

// resolveExecutionProfile is the single authoritative decision point that maps
// an execution profile to a target and auth dir, enforcing the profile's
// allowed-target constraint. Both the start-run and continue/restart-from
// paths route through it so the host constraint and auth dir hold identically
// on every path.
//
// Inputs:
//   - agentName: the effective agent for the run (already resolved by the
//     caller, since the continue path infers the agent from the prior run).
//   - requiredAgent: the agent this profile family applies to ("codex"/"claude").
//   - profileNameReq: the explicitly requested profile (may be empty).
//   - defaultProfile: the per-agent default_profile fallback.
//   - incomingTarget: the target the caller specified (--on / prior run target).
//   - lookup: resolves a profile name to its binding (nil = no profiles configured).
//
// Behavior (fail-fast, no silent fallback):
//   - The decision ALWAYS carries the incoming target (normalized: "local" ->
//     ""), so binding the decision onto opts never drops a caller-specified
//     target — the no-op paths below differ only in not applying a profile.
//   - No-op (no profile applied) unless agentName == requiredAgent.
//   - profileName = profileNameReq, falling back to defaultProfile.
//   - If profileName == "" -> no-op (agent without a configured profile).
//   - If profileName is set but lookup fails -> error.
//   - If the incoming target is empty and profile.Target != "" -> the decision's
//     Target becomes profile.Target so the run routes via the existing target path.
//   - The effective target is the resolved target name (after applying
//     profile.Target); "local"/empty is the master/local target. If
//     profile.AllowedTargets is non-empty and the effective target is not in it
//     -> error.
//   - If profile.AuthDir != "" -> decision.AuthDir = the configured path,
//     verbatim (the execution host expands a leading ~ at launch).
func resolveExecutionProfile(agentName, requiredAgent, profileNameReq, defaultProfile, incomingTarget string, lookup func(name string) (executionProfileBinding, bool)) (agentProfileDecision, error) {
	var decision agentProfileDecision

	// Start from the caller-specified target on EVERY path, including the
	// no-profile no-ops: resolving a profile must never drop the target the
	// caller asked for. A profile may only inject a target when none was given.
	effectiveTarget := strings.TrimSpace(incomingTarget)
	if effectiveTarget == "local" {
		effectiveTarget = ""
	}
	decision.Target = effectiveTarget

	if strings.TrimSpace(agentName) != requiredAgent {
		return decision, nil
	}

	profileName := strings.TrimSpace(profileNameReq)
	if profileName == "" {
		profileName = strings.TrimSpace(defaultProfile)
	}
	if profileName == "" {
		return decision, nil
	}

	var profile executionProfileBinding
	ok := false
	if lookup != nil {
		profile, ok = lookup(profileName)
	}
	if !ok {
		return decision, fmt.Errorf("unknown %s profile %q (configure it under %s.profiles)", requiredAgent, profileName, requiredAgent)
	}
	decision.ProfileName = profileName

	// Apply the profile's target only when the caller did not specify one.
	if effectiveTarget == "" && strings.TrimSpace(profile.Target) != "" {
		effectiveTarget = strings.TrimSpace(profile.Target)
		decision.Target = effectiveTarget
	}

	if len(profile.AllowedTargets) > 0 && !targetInList(effectiveTarget, profile.AllowedTargets) {
		targetLabel := effectiveTarget
		if targetLabel == "" {
			targetLabel = "local"
		}
		return decision, fmt.Errorf("%s profile %q may only run on targets %v, not %q", requiredAgent, profileName, profile.AllowedTargets, targetLabel)
	}

	if authDir := strings.TrimSpace(profile.AuthDir); authDir != "" {
		decision.AuthDir = authDir
	}

	return decision, nil
}

// resolveCodexProfile resolves a codex execution profile (account -> target ->
// CODEX_HOME) for the given agent/request/target.
func resolveCodexProfile(cfg *config.Config, agentName, profileNameReq, incomingTarget string) (agentProfileDecision, error) {
	if cfg == nil {
		return resolveExecutionProfile(agentName, "codex", "", "", incomingTarget, nil)
	}
	return resolveExecutionProfile(agentName, "codex", profileNameReq, cfg.Codex.DefaultProfile, incomingTarget, func(name string) (executionProfileBinding, bool) {
		p, ok := cfg.GetCodexProfile(name)
		if !ok {
			return executionProfileBinding{}, false
		}
		return executionProfileBinding{Target: p.Target, AllowedTargets: p.AllowedTargets, AuthDir: p.CodexHome}, true
	})
}

// resolveClaudeProfile resolves a claude execution profile (account -> target ->
// CLAUDE_CONFIG_DIR) for the given agent/request/target. Claude Code has no
// native profile CLI flag; account selection is done entirely via the
// CLAUDE_CONFIG_DIR environment variable.
func resolveClaudeProfile(cfg *config.Config, agentName, profileNameReq, incomingTarget string) (agentProfileDecision, error) {
	if cfg == nil {
		return resolveExecutionProfile(agentName, "claude", "", "", incomingTarget, nil)
	}
	return resolveExecutionProfile(agentName, "claude", profileNameReq, cfg.Claude.DefaultProfile, incomingTarget, func(name string) (executionProfileBinding, bool) {
		p, ok := cfg.GetClaudeProfile(name)
		if !ok {
			return executionProfileBinding{}, false
		}
		return executionProfileBinding{Target: p.Target, AllowedTargets: p.AllowedTargets, AuthDir: p.ConfigDir}, true
	})
}

// effectiveStartAgent resolves the agent a start-run request will launch:
// explicit option, then cfg.Agent, then "claude" (the code default).
func effectiveStartAgent(cfg *config.Config, optAgent string) string {
	agentName := strings.TrimSpace(optAgent)
	if agentName == "" && cfg != nil {
		agentName = strings.TrimSpace(cfg.Agent)
	}
	if agentName == "" {
		agentName = "claude"
	}
	return agentName
}

// effectiveContinueAgent resolves the agent a continue/restart-from request
// will launch, the same way processContinueRunCore resolves it: explicit
// override, then the prior run's agent, then cfg.Agent, then "claude".
func effectiveContinueAgent(cfg *config.Config, optAgent, fromRunAgent string) string {
	agentName := strings.TrimSpace(optAgent)
	if agentName == "" {
		agentName = strings.TrimSpace(fromRunAgent)
	}
	if agentName == "" && cfg != nil {
		agentName = strings.TrimSpace(cfg.Agent)
	}
	if agentName == "" {
		agentName = "claude"
	}
	return agentName
}

// applyCodexProfile resolves the codex profile for a start-run request and binds
// the decision onto opts (Target + CodexHome). Called at the master entry point
// BEFORE target resolution so a profile-bound target routes through worker
// delegation and AllowedTargets is enforced before any worktree is created.
func applyCodexProfile(cfg *config.Config, opts *StartRunOptions) error {
	if cfg == nil || opts == nil {
		return nil
	}

	decision, err := resolveCodexProfile(cfg, effectiveStartAgent(cfg, opts.Agent), opts.CodexProfile, opts.Target)
	if err != nil {
		return err
	}
	// Bind the RESOLVED profile name back onto opts: it captures the
	// default-profile fallback and clears a requested profile that does not
	// apply (non-codex agent), so every downstream consumer (worker payload,
	// run document) records the profile that actually took effect.
	opts.CodexProfile = decision.ProfileName
	opts.Target = decision.Target
	if decision.AuthDir != "" {
		opts.CodexHome = decision.AuthDir
	}
	return nil
}

// applyClaudeProfile resolves the claude profile for a start-run request and
// binds the decision onto opts (Target + ClaudeConfigDir). The profile is
// selected with the generic --profile flag (AgentProfile); for claude agents
// the resolved name is bound back so the run document records what took
// effect. Non-claude agents are left untouched (their AgentProfile keeps
// whatever meaning their own CLI gives it).
func applyClaudeProfile(cfg *config.Config, opts *StartRunOptions) error {
	if cfg == nil || opts == nil {
		return nil
	}
	if effectiveStartAgent(cfg, opts.Agent) != "claude" {
		return nil
	}

	decision, err := resolveClaudeProfile(cfg, "claude", opts.AgentProfile, opts.Target)
	if err != nil {
		return err
	}
	opts.AgentProfile = decision.ProfileName
	opts.Target = decision.Target
	if decision.AuthDir != "" {
		opts.ClaudeConfigDir = decision.AuthDir
	}
	return nil
}

// applyCodexProfileContinue resolves the codex profile for a
// continue/restart-from request and binds the decision onto opts. When the
// caller did not explicitly request a codex profile, a codex source run's
// recorded profile wins over codex.default_profile.
func applyCodexProfileContinue(cfg *config.Config, opts *ContinueRunOptions, fromRunAgent, fromRunProfile string) error {
	if cfg == nil || opts == nil {
		return nil
	}

	profileReq := strings.TrimSpace(opts.CodexProfile)
	if profileReq == "" && strings.TrimSpace(fromRunAgent) == "codex" {
		profileReq = strings.TrimSpace(fromRunProfile)
	}

	decision, err := resolveCodexProfile(cfg, effectiveContinueAgent(cfg, opts.Agent, fromRunAgent), profileReq, opts.Target)
	if err != nil {
		return err
	}
	// See applyCodexProfile: opts carries the resolved profile name forward.
	opts.CodexProfile = decision.ProfileName
	opts.Target = decision.Target
	if decision.AuthDir != "" {
		opts.CodexHome = decision.AuthDir
	}
	return nil
}

// applyClaudeProfileContinue resolves the claude profile for a
// continue/restart-from request and binds the decision onto opts. When the
// caller did not explicitly request a claude profile, a claude source run's
// recorded profile wins over claude.default_profile.
func applyClaudeProfileContinue(cfg *config.Config, opts *ContinueRunOptions, fromRunAgent, fromRunProfile string) error {
	if cfg == nil || opts == nil {
		return nil
	}
	if effectiveContinueAgent(cfg, opts.Agent, fromRunAgent) != "claude" {
		return nil
	}

	profileReq := strings.TrimSpace(opts.AgentProfile)
	if profileReq == "" && strings.TrimSpace(fromRunAgent) == "claude" {
		profileReq = strings.TrimSpace(fromRunProfile)
	}

	decision, err := resolveClaudeProfile(cfg, "claude", profileReq, opts.Target)
	if err != nil {
		return err
	}
	opts.AgentProfile = decision.ProfileName
	opts.Target = decision.Target
	if decision.AuthDir != "" {
		opts.ClaudeConfigDir = decision.AuthDir
	}
	return nil
}

// effectiveAgentProfile returns the profile identity a run's agent executes
// with: the resolved codex execution profile when set, otherwise the generic
// agent profile (e.g. the claude execution profile). This is what gets
// persisted on the run document and displayed by clients.
func effectiveAgentProfile(codexProfile, agentProfile string) string {
	if p := strings.TrimSpace(codexProfile); p != "" {
		return p
	}
	return strings.TrimSpace(agentProfile)
}

// targetNameForHost maps an execution host to a config.targets NAME. It
// returns the name of the first target whose Host matches the given host —
// same short-hostname/case-insensitive semantics as isLocalExecutionHost, but
// compared against the GIVEN host instead of the daemon's — or "local" when no
// configured target matches (the bare local target identity). Loopback target
// hosts (localhost/127.0.0.1/::1) designate the daemon's machine, so they
// match only when the given host is the daemon host.
// Invariant: localTargetName(cfg) == targetNameForHost(cfg, daemonHostname).
func targetNameForHost(cfg *config.Config, host string) string {
	host = strings.TrimSpace(host)
	if cfg == nil || host == "" {
		return "local"
	}
	for _, t := range cfg.Targets {
		targetHost := strings.TrimSpace(t.Host)
		if targetHost == "" || strings.Contains(targetHost, "@") {
			continue
		}
		if targetHost == "localhost" || targetHost == "127.0.0.1" || targetHost == "::1" {
			daemonHost, _ := currentDaemonHostname()
			if hostNamesEqual(daemonHost, host) {
				return strings.TrimSpace(t.Name)
			}
			continue
		}
		if hostNamesEqual(targetHost, host) {
			return strings.TrimSpace(t.Name)
		}
	}
	return "local"
}

// hostNamesEqual reports whether two hostnames identify the same machine:
// case-insensitive on the full name or on the short (first-label) name, so
// "BUILD-HOST-01" matches "build-host-01.local".
func hostNamesEqual(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if strings.EqualFold(a, b) {
		return true
	}
	return strings.EqualFold(strings.Split(a, ".")[0], strings.Split(b, ".")[0])
}

// localTargetName maps the local daemon host to a config.targets NAME, or
// "local" when no configured target matches. It is the daemon-host
// specialization of targetNameForHost.
func localTargetName(cfg *config.Config) string {
	host, _ := currentDaemonHostname()
	return targetNameForHost(cfg, host)
}

// resolveControlCodexHome resolves the CODEX_HOME for the control agent based
// on the project's default codex profile AND enforces the profile's
// AllowedTargets against the host that will EXECUTE the control agent. The
// control agent runs on the CLIENT host (orch-monitor / the CLI build and exec
// the command there), which equals the daemon host only in the single-machine
// setup; clientHost is the client-reported hostname, and an empty clientHost
// (old client) falls back to enforcing against the daemon host (back-compat).
// The host is mapped to its config.targets name (or "local"); if the profile
// constrains AllowedTargets and that target is not allowed, this fails fast
// and returns no CODEX_HOME (e.g. a company control agent must not launch on
// a disallowed host). The returned CODEX_HOME is VERBATIM as configured: a
// leading ~ is expanded on the executing (client) host at use time, never
// here. Returns an explicit error if the configured default profile name does
// not exist (fail-fast, no silent fallback).
func resolveControlCodexHome(cfg *config.Config, agentName, clientHost string) (string, error) {
	if cfg == nil {
		return "", nil
	}
	if strings.TrimSpace(agentName) != "codex" {
		return "", nil
	}
	profileName := strings.TrimSpace(cfg.Codex.DefaultProfile)
	if profileName == "" {
		return "", nil
	}
	profile, ok := cfg.GetCodexProfile(profileName)
	if !ok {
		return "", fmt.Errorf("unknown codex profile %q (configure it under codex.profiles)", profileName)
	}

	if len(profile.AllowedTargets) > 0 {
		host := strings.TrimSpace(clientHost)
		if host == "" {
			// Back-compat: old clients don't report their host; enforce
			// against the daemon host as before.
			host, _ = currentDaemonHostname()
			host = strings.TrimSpace(host)
		}
		targetName := targetNameForHost(cfg, host)
		if !targetInList(targetName, profile.AllowedTargets) {
			return "", fmt.Errorf("codex profile %q may only run on targets %v; the control agent executes on host %q (target %q), which is not allowed", profileName, profile.AllowedTargets, host, targetName)
		}
	}

	return strings.TrimSpace(profile.CodexHome), nil
}

func targetInList(target string, allowed []string) bool {
	target = strings.TrimSpace(target)
	for _, a := range allowed {
		if strings.TrimSpace(a) == target {
			return true
		}
	}
	return false
}
