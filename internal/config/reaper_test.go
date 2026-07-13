package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReaperConfigDefaultsApply(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()

	cfg, err := LoadFromProjectRoot(projectRoot)
	if err != nil {
		t.Fatalf("LoadFromProjectRoot() error = %v", err)
	}
	want := DefaultReaperConfig()
	if cfg.Reaper != want {
		t.Fatalf("Reaper = %#v, want defaults %#v", cfg.Reaper, want)
	}
}

func TestReaperConfigParsesAndMergesPerField(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.yaml")
	if err := os.WriteFile(basePath, []byte("reaper:\n  terminal_grace_minutes: 3\n  resolved_issue_grace_minutes: 4\n  idle_ttl_hours: 5\n"), 0o644); err != nil {
		t.Fatalf("write base config: %v", err)
	}
	overridePath := filepath.Join(dir, "override.yaml")
	if err := os.WriteFile(overridePath, []byte("reaper:\n  enabled: false\n  terminal_grace_minutes: 0\n"), 0o644); err != nil {
		t.Fatalf("write override config: %v", err)
	}

	cfg := &Config{Reaper: DefaultReaperConfig()}
	if err := loadFromFile(basePath, cfg); err != nil {
		t.Fatalf("load base: %v", err)
	}
	if err := loadFromFile(overridePath, cfg); err != nil {
		t.Fatalf("load override: %v", err)
	}
	if cfg.Reaper.Enabled {
		t.Fatal("explicit enabled: false was not preserved")
	}
	if cfg.Reaper.TerminalGraceMinutes != 0 || cfg.Reaper.ResolvedIssueGraceMinutes != 4 || cfg.Reaper.IdleTTLHours != 5 {
		t.Fatalf("per-field merge = %#v, want terminal=0 resolved=4 idle=5", cfg.Reaper)
	}
}

func TestReaperConfigRejectsUnknownAndNegativeValues(t *testing.T) {
	dir := t.TempDir()
	unknownPath := filepath.Join(dir, "unknown.yaml")
	if err := os.WriteFile(unknownPath, []byte("reaper:\n  terminal_grace_minute: 10\n"), 0o644); err != nil {
		t.Fatalf("write unknown config: %v", err)
	}
	if err := loadFromFile(unknownPath, &Config{Reaper: DefaultReaperConfig()}); err == nil || !strings.Contains(err.Error(), "field terminal_grace_minute not found") {
		t.Fatalf("unknown reaper key error = %v", err)
	}

	cfg := &Config{Reaper: DefaultReaperConfig()}
	cfg.Reaper.IdleTTLHours = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "reaper.idle_ttl_hours") {
		t.Fatalf("negative TTL validation error = %v", err)
	}
}
