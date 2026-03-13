package config

import (
	"bytes"
	"fmt"
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

// LoadClient loads optional client config from ~/.config/orch/client.yaml.
// Missing file is not an error.
func LoadClient() (*ClientConfig, error) {
	path := ClientConfigPath()
	if path == "" {
		return &ClientConfig{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ClientConfig{}, nil
		}
		return nil, err
	}

	var cfg ClientConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
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
