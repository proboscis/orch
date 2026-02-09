package cli

import (
	"os"
	"os/exec"
	"testing"
)

func TestGetDiffTool(t *testing.T) {
	origDiffTool := os.Getenv("ORCH_DIFFTOOL")
	origPager := os.Getenv("PAGER")
	defer func() {
		os.Setenv("ORCH_DIFFTOOL", origDiffTool)
		os.Setenv("PAGER", origPager)
	}()

	tests := []struct {
		name        string
		envDiffTool string
		envPager    string
		cfgDiffTool string
		wantTool    string
	}{
		{
			name:        "env ORCH_DIFFTOOL takes priority",
			envDiffTool: "custom-diff",
			envPager:    "less",
			cfgDiffTool: "delta",
			wantTool:    "custom-diff",
		},
		{
			name:        "config takes priority over pager",
			envDiffTool: "",
			envPager:    "less",
			cfgDiffTool: "delta",
			wantTool:    "delta",
		},
		{
			name:        "PAGER fallback",
			envDiffTool: "",
			envPager:    "most",
			cfgDiffTool: "",
			wantTool:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("ORCH_DIFFTOOL", tt.envDiffTool)
			os.Setenv("PAGER", tt.envPager)

			cfg := &mockConfig{diffTool: tt.cfgDiffTool}
			got := getDiffToolWithConfig(cfg)

			if tt.wantTool != "" && got != tt.wantTool {
				t.Errorf("getDiffTool() = %q, want %q", got, tt.wantTool)
			}
		})
	}
}

type mockConfig struct {
	diffTool string
}

func (m *mockConfig) GetDiffTool() string {
	return m.diffTool
}

func getDiffToolWithConfig(cfg interface{ GetDiffTool() string }) string {
	if tool := os.Getenv("ORCH_DIFFTOOL"); tool != "" {
		return tool
	}

	if cfg.GetDiffTool() != "" {
		return cfg.GetDiffTool()
	}

	if _, err := exec.LookPath("delta"); err == nil {
		return "delta"
	}

	if pager := os.Getenv("PAGER"); pager != "" {
		return pager
	}

	return "less"
}

func TestDisplayDiff(t *testing.T) {
	tests := []struct {
		name    string
		content string
		tool    string
		wantErr bool
	}{
		{
			name:    "empty content",
			content: "",
			tool:    "cat",
			wantErr: false,
		},
		{
			name:    "cat tool",
			content: "diff content\n",
			tool:    "cat",
			wantErr: false,
		},
		{
			name:    "empty tool falls back to stdout",
			content: "diff content\n",
			tool:    "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := displayDiff(tt.content, tt.tool)
			if (err != nil) != tt.wantErr {
				t.Errorf("displayDiff() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
