package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeClientFile writes a client.yaml at dir/rel, creating parents.
func writeClientFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// hermetic pins HOME and the working directory to temp dirs so LoadClient's
// cwd walk can never escape into the developer's real repo or global config.
func hermetic(t *testing.T) (home, work string) {
	t.Helper()
	home = t.TempDir()
	work = t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(work)
	return home, work
}

func TestLoadClientMissingFile(t *testing.T) {
	hermetic(t)

	cfg, err := LoadClient()
	if err != nil {
		t.Fatalf("LoadClient error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadClient returned nil config")
	}
}

func TestLoadClientAndResolveRemote(t *testing.T) {
	home, _ := hermetic(t)

	writeClientFile(t, home, ".config/orch/client.yaml", `remote:
  default: remotebox
  hosts:
    remotebox:
      addr: remotebox:7777
    cloud:
      addr: 10.0.0.5:7777
`)

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
	home, _ := hermetic(t)

	writeClientFile(t, home, ".config/orch/client.yaml", `remote:
  hosts:
    remotebox:
      addr: ""
`)

	_, err := LoadClient()
	if err == nil {
		t.Fatal("expected invalid client config error")
	}
}

func TestLoadClientPerRepoOverridesGlobal(t *testing.T) {
	home, work := hermetic(t)

	writeClientFile(t, home, ".config/orch/client.yaml", `remote:
  default: remotebox
  hosts:
    remotebox:
      addr: remotebox:7777
    cloud:
      addr: 10.0.0.5:7777
`)
	writeClientFile(t, work, ".orch/client.yaml", `remote:
  default: repobox
  hosts:
    repobox:
      addr: repobox:7777
    cloud:
      addr: 10.9.9.9:7777
`)

	// The per-repo file must be discovered from a nested working directory.
	nested := filepath.Join(work, "internal", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	t.Chdir(nested)

	cfg, err := LoadClient()
	if err != nil {
		t.Fatalf("LoadClient error: %v", err)
	}

	if cfg.Remote.Default != "repobox" {
		t.Fatalf("Remote.Default = %q, want repobox (per-repo wins)", cfg.Remote.Default)
	}
	if got := cfg.ResolveRemote("repobox"); got != "repobox:7777" {
		t.Fatalf("ResolveRemote(repobox) = %q, want repobox:7777", got)
	}
	if got := cfg.ResolveRemote("cloud"); got != "10.9.9.9:7777" {
		t.Fatalf("ResolveRemote(cloud) = %q, want per-repo override 10.9.9.9:7777", got)
	}
	if got := cfg.ResolveRemote("remotebox"); got != "remotebox:7777" {
		t.Fatalf("ResolveRemote(remotebox) = %q, want global alias preserved", got)
	}
}

func TestLoadClientRepoOnlyWithoutGlobal(t *testing.T) {
	_, work := hermetic(t)

	writeClientFile(t, work, ".orch/client.yaml", `remote:
  default: repobox:7777
`)

	cfg, err := LoadClient()
	if err != nil {
		t.Fatalf("LoadClient error: %v", err)
	}
	if cfg.Remote.Default != "repobox:7777" {
		t.Fatalf("Remote.Default = %q, want repobox:7777", cfg.Remote.Default)
	}
}

func TestLoadClientRepoEmptyDefaultInheritsGlobal(t *testing.T) {
	home, work := hermetic(t)

	writeClientFile(t, home, ".config/orch/client.yaml", `remote:
  default: remotebox:7777
`)
	writeClientFile(t, work, ".orch/client.yaml", `remote:
  hosts:
    extra:
      addr: extra:7777
`)

	cfg, err := LoadClient()
	if err != nil {
		t.Fatalf("LoadClient error: %v", err)
	}
	if cfg.Remote.Default != "remotebox:7777" {
		t.Fatalf("Remote.Default = %q, want global default inherited", cfg.Remote.Default)
	}
	if got := cfg.ResolveRemote("extra"); got != "extra:7777" {
		t.Fatalf("ResolveRemote(extra) = %q, want extra:7777", got)
	}
}

func TestLoadClientInvalidRepoFileNamesPath(t *testing.T) {
	_, work := hermetic(t)

	repoPath := writeClientFile(t, work, ".orch/client.yaml", `remote:
  hosts:
    broken:
      addr: ""
`)

	_, err := LoadClient()
	if err == nil {
		t.Fatal("expected invalid client config error")
	}
	if !strings.Contains(err.Error(), repoPath) {
		t.Fatalf("error %q does not name the offending file %q", err.Error(), repoPath)
	}
}

func TestLoadClientEmptyRepoFileIsIgnored(t *testing.T) {
	home, work := hermetic(t)

	writeClientFile(t, home, ".config/orch/client.yaml", `remote:
  default: remotebox:7777
`)
	writeClientFile(t, work, ".orch/client.yaml", "")

	cfg, err := LoadClient()
	if err != nil {
		t.Fatalf("LoadClient error: %v", err)
	}
	if cfg.Remote.Default != "remotebox:7777" {
		t.Fatalf("Remote.Default = %q, want global default (empty repo file ignored)", cfg.Remote.Default)
	}
}
