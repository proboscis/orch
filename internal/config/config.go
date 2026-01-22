package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/s22625/orch/internal/git"
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
	PromptTemplate string `yaml:"prompt_template,omitempty"`
}

// ClaudeConfig holds default configuration for the claude agent.
type ClaudeConfig struct {
	PromptTemplate string `yaml:"prompt_template,omitempty"`
}

// CodexConfig holds default configuration for the codex agent.
type CodexConfig struct {
	PromptTemplate string `yaml:"prompt_template,omitempty"`
}

// GeminiConfig holds default configuration for the gemini agent.
type GeminiConfig struct {
	PromptTemplate string `yaml:"prompt_template,omitempty"`
}

// SlackConfig holds configuration for Slack notifications.
type SlackConfig struct {
	Enabled    bool     `yaml:"enabled"`
	WebhookURL string   `yaml:"webhook_url,omitempty"`
	BotToken   string   `yaml:"bot_token,omitempty"`
	Channel    string   `yaml:"channel,omitempty"`
	NotifyOn   []string `yaml:"notify_on,omitempty"` // Events to notify on: blocked, blocked_api, done, failed
}

// GitHubConfig holds configuration for GitHub Issues backend.
type GitHubConfig struct {
	Owner        string            `yaml:"owner,omitempty"`        // GitHub repository owner
	Repo         string            `yaml:"repo,omitempty"`         // GitHub repository name
	LabelFilter  string            `yaml:"label_filter,omitempty"` // Only sync issues with this label (e.g., "orch")
	PollInterval int               `yaml:"poll_interval,omitempty"`
	StatusLabels map[string]string `yaml:"status_labels,omitempty"` // Map GitHub labels to status (e.g., "status:resolved" -> "resolved")
}

// IsConfigured returns true if GitHub backend has minimal required config.
func (g *GitHubConfig) IsConfigured() bool {
	return g.Owner != "" && g.Repo != ""
}

// GetPollInterval returns the poll interval with a sensible default.
func (g *GitHubConfig) GetPollInterval() int {
	if g.PollInterval <= 0 {
		return 300
	}
	return g.PollInterval
}

// IssuesConfig holds configuration for the issues backend.
type IssuesConfig struct {
	Backend string `yaml:"backend,omitempty"` // "local" (default) or "github"
	Path    string `yaml:"path,omitempty"`    // Path to issues storage (default: ~/.local/share/orch/<repo>)
}

func (s *SlackConfig) ShouldNotify(status string) bool {
	if !s.Enabled {
		return false
	}
	if len(s.NotifyOn) == 0 {
		return status == "blocked" || status == "blocked_api"
	}
	for _, n := range s.NotifyOn {
		if n == status {
			return true
		}
	}
	return false
}

func (s *SlackConfig) IsConfigured() bool {
	return s.Enabled && (s.WebhookURL != "" || (s.BotToken != "" && s.Channel != ""))
}

// Config holds orch configuration
type Config struct {
	Agent           string           `yaml:"agent"`
	Model           string           `yaml:"model"`
	ModelVariant    string           `yaml:"model_variant"`
	WorktreeDir     string           `yaml:"worktree_dir"`
	BaseBranch      string           `yaml:"base_branch"`
	PRTargetBranch  string           `yaml:"pr_target_branch"`
	LogLevel           string           `yaml:"log_level"`
	PromptTemplate     string           `yaml:"prompt_template"`
	Multiplexer        string           `yaml:"multiplexer"`         // Deprecated: use MonitorMultiplexer/AgentMultiplexer
	MonitorMultiplexer string           `yaml:"monitor_multiplexer"` // Multiplexer for orch-monitor: "zellij" (default) or "tmux"
	AgentMultiplexer   string           `yaml:"agent_multiplexer"`   // Multiplexer for agent sessions: "tmux" (default) or "zellij"
	NoPR               bool             `yaml:"no_pr"`
	Monitor         MonitorConfig    `yaml:"monitor"`
	Presets         []Preset         `yaml:"presets"`
	OpenCodePresets []OpenCodePreset `yaml:"opencode_presets"` // Deprecated: use Presets instead
	OpenCode        OpenCodeConfig   `yaml:"opencode"`
	Claude          ClaudeConfig     `yaml:"claude"`
	Codex           CodexConfig      `yaml:"codex"`
	Gemini          GeminiConfig     `yaml:"gemini"`
	DefaultPreset   string           `yaml:"default_preset"` // Default preset to use when no --preset flag is provided
	Slack           SlackConfig      `yaml:"slack"`
	Issues          IssuesConfig     `yaml:"issues"`
	GitHub          GitHubConfig     `yaml:"github"`

	// Control agent settings (for orch monitor 'c' keybinding)
	// Falls back to run agent defaults if not set
	ControlAgent        string `yaml:"control_agent"`
	ControlModel        string `yaml:"control_model"`
	ControlModelVariant string `yaml:"control_model_variant"`

	// Diff tool configuration
	DiffTool string `yaml:"diff_tool"`
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
	Multiplexer         string           `yaml:"multiplexer"`
	MonitorMultiplexer  string           `yaml:"monitor_multiplexer"`
	AgentMultiplexer    string           `yaml:"agent_multiplexer"`
	NoPR                *bool            `yaml:"no_pr"`
	Monitor             MonitorConfig    `yaml:"monitor"`
	Presets             []Preset         `yaml:"presets"`
	OpenCodePresets     []OpenCodePreset `yaml:"opencode_presets"`
	OpenCode            *OpenCodeConfig  `yaml:"opencode"`
	Claude              *ClaudeConfig    `yaml:"claude"`
	Codex               *CodexConfig     `yaml:"codex"`
	Gemini              *GeminiConfig    `yaml:"gemini"`
	DefaultPreset       string           `yaml:"default_preset"`
	Slack               *SlackConfig     `yaml:"slack"`
	Issues              *IssuesConfig    `yaml:"issues"`
	GitHub              *GitHubConfig    `yaml:"github"`
	ControlAgent        string           `yaml:"control_agent"`
	ControlModel        string           `yaml:"control_model"`
	ControlModelVariant string           `yaml:"control_model_variant"`
	DiffTool            string           `yaml:"diff_tool"`
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
// If running from a git worktree, it also includes the main repo's config.
// Returned paths are ordered from furthest ancestor to closest (highest precedence last).
func findRepoConfigs() ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	dir := cwd
	var paths []string
	visitedDirs := make(map[string]bool)
	for {
		configPath := filepath.Join(dir, ".orch", configFile)
		if _, err := os.Stat(configPath); err == nil {
			paths = append(paths, configPath)
			visitedDirs[dir] = true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if mainRepo, isWorktree := git.IsWorktree(cwd); isWorktree && !visitedDirs[mainRepo] {
		configPath := filepath.Join(mainRepo, ".orch", configFile)
		if _, err := os.Stat(configPath); err == nil {
			paths = append(paths, configPath)
		}
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

	if fileCfg.Vault != "" || fileCfg.VaultLegacy != "" || fileCfg.DefaultVault != "" {
		return fmt.Errorf("'vault' config is deprecated in %s. Use 'issues.path' instead:\n\nissues:\n  path: /path/to/issues", path)
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
		cfg.PromptTemplate = fileCfg.PromptTemplate
	}
	if fileCfg.Multiplexer != "" {
		cfg.Multiplexer = fileCfg.Multiplexer
	}
	if fileCfg.MonitorMultiplexer != "" {
		cfg.MonitorMultiplexer = fileCfg.MonitorMultiplexer
	}
	if fileCfg.AgentMultiplexer != "" {
		cfg.AgentMultiplexer = fileCfg.AgentMultiplexer
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
	if fileCfg.OpenCode != nil {
		if fileCfg.OpenCode.DefaultModel != "" {
			cfg.OpenCode.DefaultModel = fileCfg.OpenCode.DefaultModel
		}
		if fileCfg.OpenCode.DefaultVariant != "" {
			cfg.OpenCode.DefaultVariant = fileCfg.OpenCode.DefaultVariant
		}
		if fileCfg.OpenCode.PromptTemplate != "" {
			cfg.OpenCode.PromptTemplate = fileCfg.OpenCode.PromptTemplate
		}
	}
	if fileCfg.Claude != nil {
		if fileCfg.Claude.PromptTemplate != "" {
			cfg.Claude.PromptTemplate = fileCfg.Claude.PromptTemplate
		}
	}
	if fileCfg.Codex != nil {
		if fileCfg.Codex.PromptTemplate != "" {
			cfg.Codex.PromptTemplate = fileCfg.Codex.PromptTemplate
		}
	}
	if fileCfg.Gemini != nil {
		if fileCfg.Gemini.PromptTemplate != "" {
			cfg.Gemini.PromptTemplate = fileCfg.Gemini.PromptTemplate
		}
	}
	if fileCfg.DefaultPreset != "" {
		cfg.DefaultPreset = fileCfg.DefaultPreset
	}
	if fileCfg.Slack != nil {
		cfg.Slack = *fileCfg.Slack
	}
	if fileCfg.Issues != nil {
		if fileCfg.Issues.Backend != "" {
			cfg.Issues.Backend = fileCfg.Issues.Backend
		}
		if fileCfg.Issues.Path != "" {
			cfg.Issues.Path = resolvePathFromConfig(fileCfg.Issues.Path, baseDir)
		}
	}
	if fileCfg.GitHub != nil {
		if fileCfg.GitHub.Owner != "" {
			cfg.GitHub.Owner = fileCfg.GitHub.Owner
		}
		if fileCfg.GitHub.Repo != "" {
			cfg.GitHub.Repo = fileCfg.GitHub.Repo
		}
		if fileCfg.GitHub.LabelFilter != "" {
			cfg.GitHub.LabelFilter = fileCfg.GitHub.LabelFilter
		}
		if fileCfg.GitHub.PollInterval > 0 {
			cfg.GitHub.PollInterval = fileCfg.GitHub.PollInterval
		}
		if len(fileCfg.GitHub.StatusLabels) > 0 {
			cfg.GitHub.StatusLabels = fileCfg.GitHub.StatusLabels
		}
	}
	if fileCfg.ControlAgent != "" {
		cfg.ControlAgent = fileCfg.ControlAgent
	}
	if fileCfg.ControlModel != "" {
		cfg.ControlModel = fileCfg.ControlModel
	}
	if fileCfg.ControlModelVariant != "" {
	if fileCfg.DiffTool != "" {
		cfg.DiffTool = fileCfg.DiffTool
	}
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

// GetProjectRoot returns the project root directory (where .orch/ lives).
// It searches for .orch/config.yaml in the current directory and parent directories.
// Returns the directory containing .orch/, or an error if not found.
//
// Precedence:
// 1. ORCH_PROJECT_ROOT environment variable (must contain .orch/ directory)
// 2. Directory containing .orch/config.yaml (searched upward from cwd)
// 3. ORCH_VAULT as legacy fallback (only if it contains .orch/ directory)
func GetProjectRoot() (string, error) {
	if v := os.Getenv("ORCH_PROJECT_ROOT"); v != "" {
		resolved := ExpandPath(v, "")
		if hasOrchDir(resolved) {
			return resolved, nil
		}
		return "", fmt.Errorf("ORCH_PROJECT_ROOT (%s) does not contain .orch/ directory", resolved)
	}

	configPath, err := findRepoConfig()
	if err != nil {
		return "", err
	}
	if configPath != "" {
		orchDir := filepath.Dir(configPath)
		return filepath.Dir(orchDir), nil
	}

	if v := os.Getenv("ORCH_VAULT"); v != "" {
		resolved := ExpandPath(v, "")
		if hasOrchDir(resolved) {
			return resolved, nil
		}
	}

	return "", fmt.Errorf("project root not found (no .orch/config.yaml in current or parent directories)")
}

func hasOrchDir(path string) bool {
	orchPath := filepath.Join(path, ".orch")
	info, err := os.Stat(orchPath)
	return err == nil && info.IsDir()
}

// applyEnv applies environment variables to config
func applyEnv(cfg *Config) {
	if v := os.Getenv("ORCH_ISSUES_ROOT"); v != "" {
		cfg.Issues.Path = v
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
	if v := os.Getenv("ORCH_MULTIPLEXER"); v != "" {
		cfg.Multiplexer = v
	}
	if v := os.Getenv("ORCH_MONITOR_MULTIPLEXER"); v != "" {
		cfg.MonitorMultiplexer = v
	}
	if v := os.Getenv("ORCH_AGENT_MULTIPLEXER"); v != "" {
		cfg.AgentMultiplexer = v
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
	if v := os.Getenv("ORCH_DEFAULT_PRESET"); v != "" {
		cfg.DefaultPreset = v
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
	if v := os.Getenv("ORCH_SLACK_WEBHOOK_URL"); v != "" {
		cfg.Slack.WebhookURL = v
		if !cfg.Slack.Enabled {
			cfg.Slack.Enabled = true
		}
	}
	if v := os.Getenv("ORCH_SLACK_BOT_TOKEN"); v != "" {
		cfg.Slack.BotToken = v
	}
	if v := os.Getenv("ORCH_SLACK_CHANNEL"); v != "" {
		cfg.Slack.Channel = v
	}
	if v := os.Getenv("ORCH_ISSUES_BACKEND"); v != "" {
		cfg.Issues.Backend = v
	}
	if v := os.Getenv("ORCH_GITHUB_OWNER"); v != "" {
		cfg.GitHub.Owner = v
	}
	if v := os.Getenv("ORCH_GITHUB_REPO"); v != "" {
		cfg.GitHub.Repo = v
	}
	if v := os.Getenv("ORCH_GITHUB_LABEL_FILTER"); v != "" {
		cfg.GitHub.LabelFilter = v
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

func (c *Config) GetPromptTemplate(agent string) string {
	switch agent {
	case "opencode":
		if c.OpenCode.PromptTemplate != "" {
			return c.OpenCode.PromptTemplate
		}
	case "claude":
		if c.Claude.PromptTemplate != "" {
			return c.Claude.PromptTemplate
		}
	case "codex":
		if c.Codex.PromptTemplate != "" {
			return c.Codex.PromptTemplate
		}
	case "gemini":
		if c.Gemini.PromptTemplate != "" {
			return c.Gemini.PromptTemplate
		}
	}
	return c.PromptTemplate
}

// GetMultiplexer returns the legacy multiplexer setting (deprecated)
func (c *Config) GetMultiplexer() string {
	if c.Multiplexer != "" {
		return c.Multiplexer
	}
	return "auto"
}

// GetMonitorMultiplexer returns the multiplexer for orch-monitor (default: zellij)
func (c *Config) GetMonitorMultiplexer() string {
	if c.MonitorMultiplexer != "" {
		return c.MonitorMultiplexer
	}
	// Fall back to legacy Multiplexer if set
	if c.Multiplexer != "" {
		return c.Multiplexer
	}
	return "zellij"
}

// GetAgentMultiplexer returns the multiplexer for agent sessions (default: tmux)
// Note: Does NOT fall back to legacy Multiplexer field - agents must use tmux
// by default to allow attach from Zellij monitor (cross-session Zellij doesn't work)
func (c *Config) GetAgentMultiplexer() string {
	if c.AgentMultiplexer != "" {
		return c.AgentMultiplexer
	}
	return "tmux"
}

// GetBaseBranch returns the base branch with a default of "main"
func (c *Config) GetBaseBranch() string {
	if c.BaseBranch != "" {
		return c.BaseBranch
	}
	return "main"
}

// ExpandPath expands ~ and makes path absolute relative to base

// GetDiffTool returns the configured diff tool
func (c *Config) GetDiffTool() string {
	return c.DiffTool
}
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

func (c *Config) IsGitHubBackend() bool {
	return c.Issues.Backend == "github" && c.GitHub.IsConfigured()
}

func (c *Config) GetIssuesBackend() string {
	if c.Issues.Backend == "" {
		return "local"
	}
	return c.Issues.Backend
}

func (c *Config) GetIssuesPath() string {
	if c.Issues.Path != "" {
		return ExpandPath(c.Issues.Path, "")
	}

	repoID := c.getRepoID()
	if repoID == "" {
		repoID = "default"
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".local", "share", "orch", repoID)
}

func (c *Config) getRepoID() string {
	if c.GitHub.Owner != "" && c.GitHub.Repo != "" {
		return c.GitHub.Owner + "-" + c.GitHub.Repo
	}

	info, err := git.GetRepoInfo("")
	if err != nil {
		return ""
	}
	return info.ID()
}
