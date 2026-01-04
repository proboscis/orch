package agent

import (
	"strings"
)

// AgentDisplayName returns a shortened display name for an agent.
// For non-opencode agents, returns the agent name as-is.
// For opencode agents, returns a shortened form like "oc:opus4.5" based on model and variant.
//
// Examples:
//   - agent="opencode", model="anthropic/claude-opus-4-5", variant="max" → "oc:opus4.5"
//   - agent="opencode", model="anthropic/claude-opus-4-5", variant="high" → "oc:opus4.5h"
//   - agent="opencode", model="openai/gpt-5-2", variant="" → "oc:gpt5.2"
//   - agent="opencode", model="openai/gpt-5-2", variant="codex" → "oc:gpt5.2c"
//   - agent="opencode", model="google/gemini-3-pro", variant="" → "oc:gemini3-pro"
//   - agent="claude" → "claude"
func AgentDisplayName(agent, model, variant string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "-"
	}

	if agent != "opencode" {
		return agent
	}

	model = strings.TrimSpace(model)
	if model == "" {
		return "oc"
	}

	shortModel := shortenModelName(model)
	if shortModel == "" {
		return "oc"
	}

	variant = strings.TrimSpace(variant)
	suffix := variantSuffix(variant)

	result := "oc:" + shortModel + suffix
	if len(result) > MaxAgentDisplayWidth {
		return result[:MaxAgentDisplayWidth]
	}
	return result
}

func shortenModelName(model string) string {
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	model = strings.ToLower(model)

	if strings.HasPrefix(model, "claude-") {
		name := strings.TrimPrefix(model, "claude-")
		return formatClaudeModel(name)
	}

	if strings.HasPrefix(model, "gpt-") {
		name := strings.TrimPrefix(model, "gpt-")
		return "gpt" + formatVersion(name)
	}

	if strings.HasPrefix(model, "o") && len(model) >= 2 {
		rest := model[1:]
		if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			return model
		}
	}

	if strings.HasPrefix(model, "gemini-") {
		name := strings.TrimPrefix(model, "gemini-")
		return "gemini" + formatVersion(name)
	}

	if len(model) > 12 {
		return model[:12]
	}
	return model
}

func formatClaudeModel(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		return name
	}

	if isNumeric(parts[0]) {
		numericParts := []string{parts[0]}
		modelIdx := 1
		for i := 1; i < len(parts); i++ {
			if isNumeric(parts[i]) {
				numericParts = append(numericParts, parts[i])
				modelIdx = i + 1
			} else {
				break
			}
		}
		if modelIdx < len(parts) {
			modelType := parts[modelIdx]
			version := strings.Join(numericParts, ".")
			return modelType + version
		}
		return strings.Join(numericParts, ".")
	}

	modelType := parts[0]
	version := formatVersion(strings.Join(parts[1:], "-"))
	return modelType + version
}

func formatVersion(version string) string {
	if version == "" {
		return ""
	}

	parts := strings.Split(version, "-")
	if len(parts) == 1 {
		return version
	}

	allNumeric := true
	for _, p := range parts {
		if !isNumeric(p) {
			allNumeric = false
			break
		}
	}

	if allNumeric {
		return strings.Join(parts, ".")
	}

	return version
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func variantSuffix(variant string) string {
	variant = strings.ToLower(strings.TrimSpace(variant))
	switch variant {
	case "", "max":
		return ""
	case "high":
		return "h"
	case "codex":
		return "c"
	case "mini":
		return "m"
	case "low":
		return "l"
	default:
		if len(variant) > 0 {
			return string(variant[0])
		}
		return ""
	}
}

const MaxAgentDisplayWidth = 15
