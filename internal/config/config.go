package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// MonitorConfig holds configuration for the monitor dashboard.
type MonitorConfig struct {
	// PSColumns defines which columns to show and in what order.
	// Available columns: index, id, issue, issue_status, agent, status, alive,
	// branch, worktree, pr, merged, updated, topic
	PSColumns []string `yaml:"ps_columns,omitempty"`
}

// OpenCodePreset defines a configurable opencode model+variant preset.
// These presets appear in the monitor agent selection as "opencode:<name>".
// Deprecated: Use Preset with Backend="opencode" instead.
type OpenCodePreset struct {
	Name    string `yaml:"name"`    // Display name (e.g., "opus:high")
	Model   string `yaml:"model"`   // Model identifier (e.g., "anthropic/claude-opus-4-5")
	Variant string `yaml:"variant"` // Model variant (e.g., "high", "max")
}

// Preset defines a backend-agnostic preset configuration.
// Presets can be used with --preset flag in orch run or via monitor agent selection.
type Preset struct {
	Name    string `yaml:"name"`              // Display name (e.g., "opus:high", "gpt5.2-codex:xhigh")
	Backend string `yaml:"backend,omitempty"` // Backend type: opencode, claude, codex, gemini (default: opencode)
	Model   string `yaml:"model,omitempty"`   // Model identifier (backend-specific)
	Variant string `yaml:"variant,omitempty"` // Model variant (e.g., "high", "max", "xhigh")
	Profile string `yaml:"profile,omitempty"` // Agent profile (e.g., for claude --profile)
}

// EffectiveBackend returns the backend for this preset, defaulting to "opencode" if not set.
func (p *Preset) EffectiveBackend() string {
	if p.Backend == "" {
		return "opencode"
	}
	return p.Backend
}

// OpenCodeConfig holds default configuration for the opencode agent.
type OpenCodeConfig struct {
	DefaultModel   string `yaml:"default_model,omitempty"`
	DefaultVariant string `yaml:"default_variant,omitempty"`
}

// Config holds orch configuration
type Config struct {
	Vault           string           `yaml:"vault"`
	Agent           string           `yaml:"agent"`
	Model           string           `yaml:"model"`
	ModelVariant    string           `yaml:"model_variant"`
	WorktreeDir     string           `yaml:"worktree_dir"`
	BaseBranch      string           `yaml:"base_branch"`
	PRTargetBranch  string           `yaml:"pr_target_branch"`
	LogLevel        string           `yaml:"log_level"`
	PromptTemplate  string           `yaml:"prompt_template"`
	NoPR            bool             `yaml:"no_pr"`
	Monitor         MonitorConfig    `yaml:"monitor"`
	Presets         []Preset         `yaml:"presets"`
	OpenCodePresets []OpenCodePreset `yaml:"opencode_presets"` // Deprecated: use Presets instead
	OpenCode        OpenCodeConfig   `yaml:"opencode"`

	// Control agent settings (for orch monitor 'c' keybinding)
	// Falls back to run agent defaults if not set
	ControlAgent        string `yaml:"control_agent"`
	ControlModel        string `yaml:"control_model"`
	ControlModelVariant string `yaml:"control_model_variant"`
}

type fileConfig struct {
	Vault               string           `yaml:"vault"`
	VaultLegacy         string           `yaml:"Vault"`
	DefaultVault        string           `yaml:"default_vault"`
	Agent               string           `yaml:"agent"`
	Model               string           `yaml:"model"`
	ModelVariant        string           `yaml:"model_variant"`
	WorktreeDir         string           `yaml:"worktree_dir"`
	WorktreeDirLegacy   string           `yaml:"worktree_root"`
	BaseBranch          string           `yaml:"base_branch"`
	PRTargetBranch      string           `yaml:"pr_target_branch"`
	LogLevel            string           `yaml:"log_level"`
	PromptTemplate      string           `yaml:"prompt_template"`
	NoPR                *bool            `yaml:"no_pr"`
	Monitor             MonitorConfig    `yaml:"monitor"`
	Presets             []Preset         `yaml:"presets"`
	OpenCodePresets     []OpenCodePreset `yaml:"opencode_presets"`
	OpenCode            OpenCodeConfig   `yaml:"opencode"`
	ControlAgent        string           `yaml:"control_agent"`
	ControlModel        string           `yaml:"control_model"`
	ControlModelVariant string           `yaml:"control_model_variant"`
}

// configFile is the name of the config file
const configFile = "config.yaml"

// Load loads configuration with the following precedence (highest first):
// 1. Repo-local .orch/config.yaml in the current directory
// 2. Parent .orch/config.yaml files (searched upward from cwd)
// 3. Environment variables
// 4. Global ~/.config/orch/config.yaml
func Load() (*Config, error) {
	cfg := &Config{}

	// Load global config first (lowest precedence)
	globalPath := globalConfigPath()
	if globalPath != "" {
		if err := loadFromFile(globalPath, cfg); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	// Apply environment variables (higher precedence than global config)
	applyEnv(cfg)

	// Load repo-local config files (highest precedence)
	repoPaths, err := findRepoConfigs()
	if err != nil {
		return nil, err
	}
	for _, repoPath := range repoPaths {
		if err := loadFromFile(repoPath, cfg); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

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

// findRepoConfig searches upward from cwd for the closest .orch/config.yaml.
func findRepoConfig() (string, error) {
	paths, err := findRepoConfigs()
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}
	return paths[len(paths)-1], nil
}

// findRepoConfigs searches upward from cwd for .orch/config.yaml files.
// Returned paths are ordered from furthest ancestor to closest (highest precedence last).
func findRepoConfigs() ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	dir := cwd
	var paths []string
	for {
		configPath := filepath.Join(dir, ".orch", configFile)
		if _, err := os.Stat(configPath); err == nil {
			paths = append(paths, configPath)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			break
		}
		dir = parent
	}

	for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
		paths[i], paths[j] = paths[j], paths[i]
	}

	return paths, nil
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
// Relative paths for vault, worktree_root, and prompt_template are resolved
// relative to the config file's parent directory (not .orch, but the repo/home dir)
func loadFromFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// Parse into a temporary struct to merge non-empty values
	var fileCfg fileConfig
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return err
	}

	// Get the base directory for resolving relative paths
	// For .orch/config.yaml, this should be the parent of .orch (the repo root)
	// For ~/.config/orch/config.yaml, this should be ~/.config/orch
	configDir := filepath.Dir(path)
	baseDir := configDir
	if filepath.Base(configDir) == ".orch" {
		// For repo config, resolve relative to repo root (parent of .orch)
		baseDir = filepath.Dir(configDir)
	}

	// Merge: only override if value is non-empty
	// Resolve relative paths at load time
	vault := fileCfg.Vault
	if vault == "" {
		vault = fileCfg.VaultLegacy
	}
	if vault == "" {
		vault = fileCfg.DefaultVault
	}
	if vault != "" {
		cfg.Vault = resolvePathFromConfig(vault, baseDir)
	}
	if fileCfg.Agent != "" {
		cfg.Agent = fileCfg.Agent
	}
	if fileCfg.Model != "" {
		cfg.Model = fileCfg.Model
	}
	if fileCfg.ModelVariant != "" {
		cfg.ModelVariant = fileCfg.ModelVariant
	}
	worktreeDir := fileCfg.WorktreeDir
	if worktreeDir == "" {
		worktreeDir = fileCfg.WorktreeDirLegacy // Support legacy worktree_root
	}
	if worktreeDir != "" {
		cfg.WorktreeDir = resolvePathFromConfig(worktreeDir, baseDir)
	}
	if fileCfg.BaseBranch != "" {
		cfg.BaseBranch = fileCfg.BaseBranch
	}
	if fileCfg.PRTargetBranch != "" {
		cfg.PRTargetBranch = fileCfg.PRTargetBranch
	}
	if fileCfg.LogLevel != "" {
		cfg.LogLevel = fileCfg.LogLevel
	}
	if fileCfg.PromptTemplate != "" {
		cfg.PromptTemplate = resolvePathFromConfig(fileCfg.PromptTemplate, baseDir)
	}
	if fileCfg.NoPR != nil {
		cfg.NoPR = *fileCfg.NoPR
	}
	if len(fileCfg.Monitor.PSColumns) > 0 {
		cfg.Monitor.PSColumns = fileCfg.Monitor.PSColumns
	}
	if len(fileCfg.Presets) > 0 {
		cfg.Presets = fileCfg.Presets
	}
	if len(fileCfg.OpenCodePresets) > 0 {
		cfg.OpenCodePresets = fileCfg.OpenCodePresets
	}
	if fileCfg.OpenCode.DefaultModel != "" {
		cfg.OpenCode.DefaultModel = fileCfg.OpenCode.DefaultModel
	}
	if fileCfg.OpenCode.DefaultVariant != "" {
		cfg.OpenCode.DefaultVariant = fileCfg.OpenCode.DefaultVariant
	}
	if fileCfg.ControlAgent != "" {
		cfg.ControlAgent = fileCfg.ControlAgent
	}
	if fileCfg.ControlModel != "" {
		cfg.ControlModel = fileCfg.ControlModel
	}
	if fileCfg.ControlModelVariant != "" {
		cfg.ControlModelVariant = fileCfg.ControlModelVariant
	}

	return nil
}

// resolvePathFromConfig resolves a path from a config file
// - Expands ~ to home directory
// - Makes relative paths absolute relative to baseDir
// - Returns absolute paths unchanged
func resolvePathFromConfig(path, baseDir string) string {
	if path == "" {
		return ""
	}

	// Expand ~
	if len(path) > 0 && path[0] == '~' {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}

	// Make relative paths absolute
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}

	return path
}

// applyEnv applies environment variables to config
func applyEnv(cfg *Config) {
	if v := os.Getenv("ORCH_VAULT"); v != "" {
		cfg.Vault = v
	}
	if v := os.Getenv("ORCH_AGENT"); v != "" {
		cfg.Agent = v
	}
	if v := os.Getenv("ORCH_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("ORCH_MODEL_VARIANT"); v != "" {
		cfg.ModelVariant = v
	}
	if v := os.Getenv("ORCH_WORKTREE_DIR"); v != "" {
		cfg.WorktreeDir = v
	} else if v := os.Getenv("ORCH_WORKTREE_ROOT"); v != "" {
		cfg.WorktreeDir = v // Support legacy env var
	}
	if v := os.Getenv("ORCH_BASE_BRANCH"); v != "" {
		cfg.BaseBranch = v
	}
	if v := os.Getenv("ORCH_PR_TARGET_BRANCH"); v != "" {
		cfg.PRTargetBranch = v
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
	if v := os.Getenv("ORCH_OPENCODE_DEFAULT_MODEL"); v != "" {
		cfg.OpenCode.DefaultModel = v
	}
	if v := os.Getenv("ORCH_OPENCODE_DEFAULT_VARIANT"); v != "" {
		cfg.OpenCode.DefaultVariant = v
	}
	if v := os.Getenv("ORCH_CONTROL_AGENT"); v != "" {
		cfg.ControlAgent = v
	}
	if v := os.Getenv("ORCH_CONTROL_MODEL"); v != "" {
		cfg.ControlModel = v
	}
	if v := os.Getenv("ORCH_CONTROL_MODEL_VARIANT"); v != "" {
		cfg.ControlModelVariant = v
	}
}

// GetOpenCodePreset returns the preset with the given name, or nil if not found.
// Deprecated: Use GetPreset instead.
func (c *Config) GetOpenCodePreset(name string) *OpenCodePreset {
	for i := range c.OpenCodePresets {
		if c.OpenCodePresets[i].Name == name {
			return &c.OpenCodePresets[i]
		}
	}
	return nil
}

// GetPreset returns the preset with the given name, searching both
// new-style Presets and legacy OpenCodePresets (with backward compatibility).
func (c *Config) GetPreset(name string) *Preset {
	for i := range c.Presets {
		if c.Presets[i].Name == name {
			return &c.Presets[i]
		}
	}
	for i := range c.OpenCodePresets {
		if c.OpenCodePresets[i].Name == name {
			return &Preset{
				Name:    c.OpenCodePresets[i].Name,
				Backend: "opencode",
				Model:   c.OpenCodePresets[i].Model,
				Variant: c.OpenCodePresets[i].Variant,
			}
		}
	}
	return nil
}

// GetAllPresets returns all presets, merging new-style Presets with
// legacy OpenCodePresets (converted to Preset format). Results are sorted by name.
func (c *Config) GetAllPresets() []Preset {
	presetMap := make(map[string]Preset)
	for _, p := range c.Presets {
		presetMap[p.Name] = p
	}
	for _, p := range c.OpenCodePresets {
		if _, exists := presetMap[p.Name]; !exists {
			presetMap[p.Name] = Preset{
				Name:    p.Name,
				Backend: "opencode",
				Model:   p.Model,
				Variant: p.Variant,
			}
		}
	}
	result := make([]Preset, 0, len(presetMap))
	for _, p := range presetMap {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// GetPresetsForBackend returns all presets for a specific backend.
func (c *Config) GetPresetsForBackend(backend string) []Preset {
	var result []Preset
	for _, p := range c.GetAllPresets() {
		if p.EffectiveBackend() == backend {
			result = append(result, p)
		}
	}
	return result
}

var validBackends = map[string]bool{
	"opencode": true,
	"claude":   true,
	"codex":    true,
	"gemini":   true,
	"custom":   true,
}

// ValidatePresets checks all presets for valid backends and configuration.
func (c *Config) ValidatePresets() []string {
	var warnings []string
	for _, p := range c.Presets {
		if p.Name == "" {
			warnings = append(warnings, "preset has empty name")
			continue
		}
		backend := p.EffectiveBackend()
		if !validBackends[backend] {
			warnings = append(warnings, fmt.Sprintf("preset %q has invalid backend %q", p.Name, backend))
		}
	}
	if len(c.OpenCodePresets) > 0 && len(c.Presets) == 0 {
		warnings = append(warnings, "opencode_presets is deprecated, migrate to presets with backend field")
	}
	return warnings
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
