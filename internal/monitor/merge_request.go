package monitor

import (
	"fmt"
	"strings"

	"github.com/s22625/orch/internal/model"
)

// RequestMerge returns PR status for the run. PR creation is handled by the
// coding agent, not the monitor. Use "orch send" to ask the agent to create a PR.
func (m *Monitor) RequestMerge(run *model.Run) (string, error) {
	if run == nil {
		return "", fmt.Errorf("run not found")
	}

	if run.PRUrl != "" {
		state := strings.ToLower(run.PRState)
		if state == "" {
			state = "unknown"
		}
		return fmt.Sprintf("PR %s (%s)", run.PRUrl, state), nil
	}

	ref := run.Ref().String()
	return fmt.Sprintf("No PR found. Ask the agent to create one:\n  orch send %s \"Please create a PR for your changes\"", ref), nil
}
