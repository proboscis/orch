package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadClientMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := LoadClient()
	if err != nil {
		t.Fatalf("LoadClient error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadClient returned nil config")
	}
}

func TestLoadClientAndResolveRemote(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgDir := filepath.Join(home, ".config", "orch")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir client config dir: %v", err)
	}

	content := []byte(`remote:
  default: remotebox
  hosts:
    remotebox:
      addr: remotebox:7777
    cloud:
      addr: 10.0.0.5:7777
`)
	if err := os.WriteFile(filepath.Join(cfgDir, "client.yaml"), content, 0o644); err != nil {
		t.Fatalf("write client config: %v", err)
	}

	cfg, err := LoadClient()
	if err != nil {
		t.Fatalf("LoadClient error: %v", err)
	}

	if got := cfg.ResolveRemote(cfg.Remote.Default); got != "remotebox:7777" {
		t.Fatalf("ResolveRemote(default) = %q, want remotebox:7777", got)
	}
	if got := cfg.ResolveRemote("cloud"); got != "10.0.0.5:7777" {
		t.Fatalf("ResolveRemote(cloud) = %q, want 10.0.0.5:7777", got)
	}
	if got := cfg.ResolveRemote("custom:9999"); got != "custom:9999" {
		t.Fatalf("ResolveRemote(custom:9999) = %q, want custom:9999", got)
	}
}

func TestLoadClientRejectsHostWithoutAddr(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgDir := filepath.Join(home, ".config", "orch")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir client config dir: %v", err)
	}

	content := []byte(`remote:
  hosts:
    remotebox:
      addr: ""
`)
	if err := os.WriteFile(filepath.Join(cfgDir, "client.yaml"), content, 0o644); err != nil {
		t.Fatalf("write client config: %v", err)
	}

	_, err := LoadClient()
	if err == nil {
		t.Fatal("expected invalid client config error")
	}
}
