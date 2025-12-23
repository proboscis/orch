package agent

// KnownModels returns a list of commonly available model names for an agent.
// The list is intentionally small; callers should allow custom input.
func KnownModels(agentType AgentType) []string {
	switch agentType {
	case AgentCodex:
		return []string{
			"gpt-5.2-codex",
			"gpt-5.2",
			"gpt-5.1-codex-max",
			"gpt-5.1-codex",
			"gpt-5.1",
			"o3",
			"o4-mini",
			"o1",
			"gpt-4o",
			"gpt-4o-mini",
		}
	case AgentClaude:
		return []string{
			"sonnet",
			"opus",
			"haiku",
			"claude-sonnet-4-5-20250929",
			"claude-3-5-sonnet-20241022",
			"claude-3-opus-latest",
			"claude-3-5-haiku-20241022",
		}
	case AgentGemini:
		return []string{
			"gemini-1.5-pro",
			"gemini-1.5-flash",
			"gemini-2.0-flash-exp",
		}
	default:
		return nil
	}
}
