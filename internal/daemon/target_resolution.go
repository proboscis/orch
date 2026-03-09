package daemon

import (
	"fmt"
	"strings"
)

type resolvedTarget struct {
	Name     string
	Host     string
	WorkerID string
}

func resolveTargetForProjectRoot(projectRoot, targetName string) (*resolvedTarget, error) {
	targetName = strings.TrimSpace(targetName)
	if targetName == "" || targetName == "local" {
		return nil, nil
	}

	cfg, err := loadConfigForProjectRoot(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load config for target %q: %w", targetName, err)
	}

	targetCfg := cfg.GetTarget(targetName)
	if targetCfg == nil {
		return nil, fmt.Errorf("target %q not found in config", targetName)
	}

	host := strings.TrimSpace(targetCfg.Host)
	if host == "" {
		return nil, fmt.Errorf("target %q host is empty", targetName)
	}

	return &resolvedTarget{
		Name:     targetName,
		Host:     host,
		WorkerID: HostWorkerID(host),
	}, nil
}
