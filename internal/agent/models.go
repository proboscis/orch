package agent

// KnownModels returns a list of commonly available model names for an agent.
// The list is intentionally small; callers should allow custom input.
func KnownModels(agentType AgentType) []string {
	switch agentType {
	case AgentCodex:
		return []string{
			"o3",
			"o1",
			"gpt-4o",
			"gpt-4o-mini",
		}
	case AgentClaude:
		return []string{
			"sonnet",
			"opus",
			"haiku",
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
