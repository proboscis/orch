package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds orch configuration
type Config struct {
	Vault          string `yaml:"vault"`
	Agent          string `yaml:"agent"`
	WorktreeRoot   string `yaml:"worktree_root"`
	BaseBranch     string `yaml:"base_branch"`
	LogLevel       string `yaml:"log_level"`
	PromptTemplate string `yaml:"prompt_template"` // Path to custom prompt template
	NoPR           bool   `yaml:"no_pr"`           // Disable PR instructions by default
}

// configFile is the name of the config file
const configFile = "config.yaml"

// Load loads configuration with the following precedence (highest first):
// 1. Environment variables
// 2. Repo-local .orch/config.yaml (searched upward from cwd)
// 3. Global ~/.config/orch/config.yaml
func Load() (*Config, error) {
	cfg := &Config{}

	// Load global config first (lowest precedence)
	globalPath := globalConfigPath()
	if globalPath != "" {
		if err := loadFromFile(globalPath, cfg); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	// Load repo-local config (higher precedence)
	repoPath, _ := findRepoConfig()
	if repoPath != "" {
		if err := loadFromFile(repoPath, cfg); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	// Apply environment variables (highest precedence)
	applyEnv(cfg)

	return cfg, nil
}

// RepoConfigDir returns the path to .orch directory if found, empty string otherwise
func RepoConfigDir() string {
	configPath, _ := findRepoConfig()
	if configPath == "" {
		return ""
	}
	return filepath.Dir(configPath)
}

// findRepoConfig searches upward from cwd for .orch/config.yaml
func findRepoConfig() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		configPath := filepath.Join(dir, ".orch", configFile)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			break
		}
		dir = parent
	}

	return "", nil
}

// globalConfigPath returns the path to global config
func globalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "orch", configFile)
}

// loadFromFile loads config from a YAML file, merging into existing cfg
func loadFromFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Parse into a temporary struct to merge non-empty values
	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return err
	}

	// Merge: only override if value is non-empty
	if fileCfg.Vault != "" {
		cfg.Vault = fileCfg.Vault
	}
	if fileCfg.Agent != "" {
		cfg.Agent = fileCfg.Agent
	}
	if fileCfg.WorktreeRoot != "" {
		cfg.WorktreeRoot = fileCfg.WorktreeRoot
	}
	if fileCfg.BaseBranch != "" {
		cfg.BaseBranch = fileCfg.BaseBranch
	}
	if fileCfg.LogLevel != "" {
		cfg.LogLevel = fileCfg.LogLevel
	}
	if fileCfg.PromptTemplate != "" {
		cfg.PromptTemplate = fileCfg.PromptTemplate
	}
	// NoPR is a boolean, so we need special handling - yaml will parse it
	// For now, let the yaml directly merge it
	cfg.NoPR = fileCfg.NoPR

	return nil
}

// applyEnv applies environment variables to config
func applyEnv(cfg *Config) {
	if v := os.Getenv("ORCH_VAULT"); v != "" {
		cfg.Vault = v
	}
	if v := os.Getenv("ORCH_AGENT"); v != "" {
		cfg.Agent = v
	}
	if v := os.Getenv("ORCH_WORKTREE_ROOT"); v != "" {
		cfg.WorktreeRoot = v
	}
	if v := os.Getenv("ORCH_BASE_BRANCH"); v != "" {
		cfg.BaseBranch = v
	}
	if v := os.Getenv("ORCH_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("ORCH_PROMPT_TEMPLATE"); v != "" {
		cfg.PromptTemplate = v
	}
	if v := os.Getenv("ORCH_NO_PR"); v != "" {
		cfg.NoPR = v == "true" || v == "1" || v == "yes"
	}
}

// ExpandPath expands ~ and makes path absolute relative to base
func ExpandPath(path, base string) string {
	if path == "" {
		return ""
	}

	// Expand ~
	if len(path) > 0 && path[0] == '~' {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}

	// Make absolute if relative
	if !filepath.IsAbs(path) && base != "" {
		path = filepath.Join(base, path)
	}

	return path
}
