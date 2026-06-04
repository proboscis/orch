package daemon

import (
	"fmt"
	"strings"

	"github.com/s22625/orch/internal/config"
)

// codexProfileDecision is the resolved outcome of selecting a codex profile.
type codexProfileDecision struct {
	// Target is the (possibly profile-injected) config.targets name the run
	// should execute on. Empty means master/local execution.
	Target string
	// CodexHome is the CODEX_HOME for the selected profile (after ~ expansion).
	// Empty means use the agent default (~/.codex).
	CodexHome string
}

// resolveCodexProfile is the single authoritative decision point that maps a
// codex profile to an execution target and CODEX_HOME, enforcing the profile's
// allowed-target constraint. Both the start-run and continue/restart-from paths
// route through it so the host constraint and CODEX_HOME hold identically on
// every path.
//
// Inputs:
//   - agentName: the effective agent for the run (already resolved by the
//     caller, since the continue path infers the agent from the prior run).
//   - profileNameReq: the explicitly requested profile (may be empty).
//   - incomingTarget: the target the caller specified (--on / prior run target).
//
// Behavior (fail-fast, no silent fallback):
//   - No-op (returns zero decision, nil) unless agentName == "codex".
//   - profileName = profileNameReq, falling back to cfg.Codex.DefaultProfile.
//   - If profileName == "" -> no-op (codex without a configured profile).
//   - If profileName is set but not present in cfg.Codex.Profiles -> error.
//   - If the incoming target is empty and profile.Target != "" -> the decision's
//     Target becomes profile.Target so the run routes via the existing target path.
//   - The effective target is the resolved target name (after applying
//     profile.Target); "local"/empty is the master/local target. If
//     profile.AllowedTargets is non-empty and the effective target is not in it
//     -> error.
//   - If profile.CodexHome != "" -> decision.CodexHome = expanded CODEX_HOME.
func resolveCodexProfile(cfg *config.Config, agentName, profileNameReq, incomingTarget string) (codexProfileDecision, error) {
	var decision codexProfileDecision
	if cfg == nil {
		return decision, nil
	}

	agentName = strings.TrimSpace(agentName)
	if agentName != "codex" {
		return decision, nil
	}

	profileName := strings.TrimSpace(profileNameReq)
	if profileName == "" {
		profileName = strings.TrimSpace(cfg.Codex.DefaultProfile)
	}
	if profileName == "" {
		return decision, nil
	}

	profile, ok := cfg.GetCodexProfile(profileName)
	if !ok {
		return decision, fmt.Errorf("unknown codex profile %q (configure it under codex.profiles)", profileName)
	}

	// Start from the caller-specified target.
	effectiveTarget := strings.TrimSpace(incomingTarget)
	if effectiveTarget == "local" {
		effectiveTarget = ""
	}
	decision.Target = effectiveTarget

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
		return decision, fmt.Errorf("codex profile %q may only run on targets %v, not %q", profileName, profile.AllowedTargets, targetLabel)
	}

	if strings.TrimSpace(profile.CodexHome) != "" {
		decision.CodexHome = config.ExpandPath(strings.TrimSpace(profile.CodexHome), "")
	}

	return decision, nil
}

// applyCodexProfile resolves the codex profile for a start-run request and binds
// the decision onto opts (Target + CodexHome). Called at the master entry point
// BEFORE target resolution so a profile-bound target routes through worker
// delegation and AllowedTargets is enforced before any worktree is created.
func applyCodexProfile(cfg *config.Config, opts *StartRunOptions) error {
	if cfg == nil || opts == nil {
		return nil
	}

	agentName := strings.TrimSpace(opts.Agent)
	if agentName == "" {
		agentName = strings.TrimSpace(cfg.Agent)
	}
	if agentName == "" {
		agentName = "claude"
	}

	decision, err := resolveCodexProfile(cfg, agentName, opts.CodexProfile, opts.Target)
	if err != nil {
		return err
	}
	opts.Target = decision.Target
	if decision.CodexHome != "" {
		opts.CodexHome = decision.CodexHome
	}
	return nil
}

// applyCodexProfileContinue resolves the codex profile for a continue/restart-from
// request and binds the decision onto opts. The effective agent is resolved the
// same way processContinueRunCore resolves it: explicit override, then the prior
// run's agent, then cfg.Agent, then "claude".
func applyCodexProfileContinue(cfg *config.Config, opts *ContinueRunOptions, fromRunAgent string) error {
	if cfg == nil || opts == nil {
		return nil
	}

	agentName := strings.TrimSpace(opts.Agent)
	if agentName == "" {
		agentName = strings.TrimSpace(fromRunAgent)
	}
	if agentName == "" {
		agentName = strings.TrimSpace(cfg.Agent)
	}
	if agentName == "" {
		agentName = "claude"
	}

	decision, err := resolveCodexProfile(cfg, agentName, opts.CodexProfile, opts.Target)
	if err != nil {
		return err
	}
	opts.Target = decision.Target
	if decision.CodexHome != "" {
		opts.CodexHome = decision.CodexHome
	}
	return nil
}

// localTargetName maps the local daemon host to a config.targets NAME. It returns
// the name of the first target whose Host resolves to the local daemon host (via
// isLocalExecutionHost), or "local" when no configured target matches the local
// host (the bare local target identity).
func localTargetName(cfg *config.Config) string {
	if cfg != nil {
		for _, t := range cfg.Targets {
			if isLocalExecutionHost(strings.TrimSpace(t.Host)) {
				return strings.TrimSpace(t.Name)
			}
		}
	}
	return "local"
}

// resolveControlCodexHome resolves the CODEX_HOME for the control agent based on
// the project's default codex profile AND enforces the profile's AllowedTargets
// against the LOCAL daemon host. The control agent always runs locally, so the
// local host is mapped to its config.targets name (or "local"); if the profile
// constrains AllowedTargets and the local target is not allowed, this fails fast
// and returns no CODEX_HOME (e.g. a company control agent must not launch on
// zeus). Returns an explicit error if the configured default profile name does
// not exist (fail-fast, no silent fallback).
func resolveControlCodexHome(cfg *config.Config, agentName string) (string, error) {
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

	// Enforce AllowedTargets against the local daemon host (the control agent is
	// always local). The bare local host maps to its config.targets name, or
	// "local" when no target matches.
	if len(profile.AllowedTargets) > 0 {
		localTarget := localTargetName(cfg)
		if !targetInList(localTarget, profile.AllowedTargets) {
			return "", fmt.Errorf("codex profile %q may only run on targets %v, not local host (target %q); the control agent runs locally and cannot launch this profile on this host", profileName, profile.AllowedTargets, localTarget)
		}
	}

	if strings.TrimSpace(profile.CodexHome) == "" {
		return "", nil
	}
	return config.ExpandPath(strings.TrimSpace(profile.CodexHome), ""), nil
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
