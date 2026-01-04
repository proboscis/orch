package cli

import (
	"fmt"
	"os"
)

type DebugLogger struct {
	enabled bool
}

func NewDebugLogger() *DebugLogger {
	enabled := globalOpts.LogLevel == "debug" ||
		os.Getenv("ORCH_DEBUG") == "1" ||
		os.Getenv("ORCH_DEBUG") == "true" ||
		os.Getenv("ORCH_DEBUG") == "yes"
	return &DebugLogger{enabled: enabled}
}

func (d *DebugLogger) IsEnabled() bool {
	return d.enabled
}

func (d *DebugLogger) Printf(format string, args ...interface{}) {
	if !d.enabled {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "[DEBUG] %s\n", msg)
}
