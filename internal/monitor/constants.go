package monitor

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultRefreshInterval  = 5 * time.Second
	defaultCaptureLines     = 200
	runTableTargetWidth     = 10
	runTableTargetHostWidth = 16
	runTableBranchWidth     = 8
	runTableWorktreeWidth   = 16
	runDetailsMaxLines      = 7
)

const monitorRefreshIntervalEnv = "ORCH_MONITOR_REFRESH_INTERVAL"

func monitorRefreshInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv(monitorRefreshIntervalEnv))
	if raw == "" {
		return defaultRefreshInterval
	}

	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return defaultRefreshInterval
	}
	return parsed
}
