package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenInObsidianBuildsURI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows uses cmd /c start")
	}

	dir := t.TempDir()
	cmdName := "open"
	if runtime.GOOS == "linux" {
		cmdName = "xdg-open"
	}

	captureFile := filepath.Join(dir, "capture.txt")
	scriptPath := filepath.Join(dir, cmdName)
	script := "#!/bin/sh\nprintf \"%s\" \"$1\" > \"$ORCH_OPEN_CAPTURE\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ORCH_OPEN_CAPTURE", captureFile)

	vaultPath := filepath.Join(dir, "vault")
	if err := os.MkdirAll(filepath.Join(vaultPath, "issues"), 0755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	path := filepath.Join(vaultPath, "issues", "issue.md")

	if err := openInObsidian(path, vaultPath); err != nil {
		t.Fatalf("openInObsidian: %v", err)
	}

	data, err := os.ReadFile(captureFile)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}

	want := fmt.Sprintf("obsidian://open?vault=%s&file=issues/issue.md", filepath.Base(vaultPath))
	if string(data) != want {
		t.Fatalf("uri = %q, want %q", string(data), want)
	}
}
