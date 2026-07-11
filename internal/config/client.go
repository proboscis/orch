package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const clientConfigFile = "client.yaml"

type ClientRemoteHost struct {
	Addr string `yaml:"addr"`
}

type ClientRemoteConfig struct {
	Default string                      `yaml:"default"`
	Hosts   map[string]ClientRemoteHost `yaml:"hosts"`
}

type ClientConfig struct {
	Remote ClientRemoteConfig `yaml:"remote"`
}

// ClientConfigPath returns ~/.config/orch/client.yaml.
func ClientConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "orch", clientConfigFile)
}

// LoadClient loads optional client config. Two locations are consulted:
// a per-repo .orch/client.yaml discovered by walking up from the current
// directory, and the global ~/.config/orch/client.yaml. Per-repo values win
// field-wise: a non-empty remote.default overrides the global one, and host
// aliases are merged with per-repo entries taking precedence. Missing files
// are not an error.
func LoadClient() (*ClientConfig, error) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	return loadClientFrom(cwd)
}

func loadClientFrom(startDir string) (*ClientConfig, error) {
	merged := &ClientConfig{}
	global, err := loadClientFile(ClientConfigPath())
	if err != nil {
		return nil, err
	}
	if global != nil {
		merged = global
	}

	repoPath := findRepoClientConfig(startDir)
	if repoPath == "" {
		return merged, nil
	}
	repo, err := loadClientFile(repoPath)
	if err != nil {
		return nil, err
	}
	if repo == nil {
		return merged, nil
	}

	if d := strings.TrimSpace(repo.Remote.Default); d != "" {
		merged.Remote.Default = d
	}
	if len(repo.Remote.Hosts) > 0 {
		if merged.Remote.Hosts == nil {
			merged.Remote.Hosts = make(map[string]ClientRemoteHost, len(repo.Remote.Hosts))
		}
		for name, host := range repo.Remote.Hosts {
			merged.Remote.Hosts[name] = host
		}
	}
	return merged, nil
}

// findRepoClientConfig walks up from startDir looking for .orch/client.yaml,
// mirroring the project-config discovery of .orch/config.yaml. The repo does
// not need a .orch/config.yaml for its client.yaml to apply.
func findRepoClientConfig(startDir string) string {
	if startDir == "" {
		return ""
	}
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, ".orch", clientConfigFile)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// loadClientFile reads and validates one client config file. A missing (or
// empty) file yields (nil, nil); a present-but-invalid file is an error that
// names the offending path.
func loadClientFile(path string) (*ClientConfig, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cfg ClientConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("invalid client config schema in %s: %w", path, err)
	}

	for name, host := range cfg.Remote.Hosts {
		addr := strings.TrimSpace(host.Addr)
		if addr == "" {
			return nil, fmt.Errorf("invalid client config in %s: remote.hosts.%s.addr is required", path, name)
		}
	}

	return &cfg, nil
}

// ResolveRemote resolves a host alias to addr if present; otherwise returns the
// value as-is (trimmed). Empty input returns empty output.
func (c *ClientConfig) ResolveRemote(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if c != nil {
		if host, ok := c.Remote.Hosts[v]; ok {
			if addr := strings.TrimSpace(host.Addr); addr != "" {
				return addr
			}
		}
	}
	return v
}
