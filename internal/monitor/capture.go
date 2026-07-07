package monitor

import (
	"fmt"
	"strings"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/model"
)

func (m *Monitor) CaptureRun(run *model.Run, lines int) (string, error) {
	if run == nil {
		return "", fmt.Errorf("run not found")
	}
	if lines <= 0 {
		lines = defaultCaptureLines
	}

	mgr := agent.GetManager(run)
	content, err := mgr.CaptureOutput(run)
	if err != nil {
		return "", err
	}

	return strings.TrimRight(content, "\n"), nil
}
