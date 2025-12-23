package model

import (
	"strings"
	"unicode"
)

// ShortModelAlias returns a compact model identifier for display.
func ShortModelAlias(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}

	base := model
	for _, prefix := range []string{"gpt-", "claude-", "gemini-"} {
		if strings.HasPrefix(base, prefix) {
			base = strings.TrimPrefix(base, prefix)
			break
		}
	}

	base = strings.TrimSuffix(base, "-latest")

	parts := strings.Split(base, "-")
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		if isDateToken(last) {
			parts = parts[:len(parts)-1]
			base = strings.Join(parts, "-")
		}
	}

	if len(parts) >= 3 && isFamilyToken(parts[0]) && isDigits(parts[1]) && isDigits(parts[2]) {
		version := parts[1] + "." + parts[2]
		rest := parts[3:]
		base = version + "-" + parts[0]
		if len(rest) > 0 {
			base += "-" + strings.Join(rest, "-")
		}
		return base
	}

	if len(parts) >= 2 && isDigits(parts[0]) && isDigits(parts[1]) {
		version := parts[0] + "." + parts[1]
		rest := parts[2:]
		base = version
		if len(rest) > 0 {
			base += "-" + strings.Join(rest, "-")
		}
	}

	return base
}

// ShortThinkingAlias returns a compact thinking/reasoning label for display.
func ShortThinkingAlias(thinking string) string {
	return strings.ToLower(strings.TrimSpace(thinking))
}

// ModelThinkingAlias combines model and thinking aliases for display.
func ModelThinkingAlias(model, thinking string) string {
	modelAlias := ShortModelAlias(model)
	thinkingAlias := ShortThinkingAlias(thinking)
	if modelAlias == "" {
		return thinkingAlias
	}
	if thinkingAlias == "" {
		return modelAlias
	}
	return modelAlias + "-" + thinkingAlias
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isDateToken(value string) bool {
	if len(value) != 8 {
		return false
	}
	return isDigits(value)
}

func isFamilyToken(value string) bool {
	switch value {
	case "sonnet", "opus", "haiku":
		return true
	default:
		return false
	}
}
