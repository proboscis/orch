package daemon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/proboscis/orch/internal/model"
)

// loadReapedSessionSnapshot serves the final capture recorded by the reaper.
// It is intentionally side-effect free: capture on a reaped run must never
// resolve, boot, lease, or probe a multiplexer session.
func loadReapedSessionSnapshot(run *model.Run) (string, time.Time, error) {
	if run == nil {
		return "", time.Time{}, fmt.Errorf("run required")
	}
	if !run.SessionReaped() {
		return "", time.Time{}, fmt.Errorf("run %s session is not reaped", run.Ref().String())
	}

	reapedGeneration := run.ReapedSessionGeneration()
	reapNoteIndex := -1
	var reapNote *model.Event
	for i := len(run.Events) - 1; i >= 0; i-- {
		event := run.Events[i]
		if event == nil || event.Type != model.EventTypeNote || event.Name != model.DaemonNoticeEventName || event.Attrs["kind"] != "session_reaped" {
			continue
		}
		generation, err := strconv.Atoi(event.Attrs["generation"])
		if err == nil && generation == reapedGeneration {
			reapNoteIndex = i
			reapNote = event
			break
		}
	}
	if reapNote == nil {
		return "", time.Time{}, fmt.Errorf("reaped run %s has no session_reaped note for generation %d", run.Ref().String(), reapedGeneration)
	}

	var snapshot *model.Event
	for i := reapNoteIndex - 1; i >= 0; i-- {
		event := run.Events[i]
		if event != nil && event.Type == model.EventTypeArtifact && event.Name == "session_snapshot" {
			snapshot = event
			break
		}
	}
	if snapshot == nil {
		return "", time.Time{}, fmt.Errorf("reaped run %s has no session_snapshot artifact for generation %d", run.Ref().String(), reapedGeneration)
	}

	path := strings.TrimSpace(snapshot.Attrs["path"])
	if path == "" {
		return "", time.Time{}, fmt.Errorf("reaped run %s session_snapshot artifact has empty sidecar path", run.Ref().String())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read session_snapshot sidecar %s for reaped run %s: %w", path, run.Ref().String(), err)
	}

	header := fmt.Sprintf(
		"session reaped at %s (reason=%s); serving final snapshot",
		reapNote.Timestamp.UTC().Format(time.RFC3339Nano),
		reapNote.Attrs["reason"],
	)
	return header + "\n" + string(content), reapNote.Timestamp, nil
}
