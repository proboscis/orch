package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/s22625/orch/internal/git"
	"gopkg.in/yaml.v3"
)

// MonitorConfig holds configuration for the monitor dashboard.
type MonitorConfig struct {
	// PSColumns defines which columns to show and in what order.
	// Available columns: index, id, issue, issue_status, agent, status, alive,
	// branch, worktree, pr, merged, updated, topic
	PSColumns []string `yaml:"ps_columns,omitempty"`

	// Python monitor-tui defaults (accepted for cross-client config compatibility).
	DefaultRunStatuses   []string                  `yaml:"default_run_statuses,omitempty"`
	DefaultIssueStatuses []string                  `yaml:"default_issue_statuses,omitempty"`
	DefaultIssueFilter   MonitorIssueDefaultFilter `yaml:"default_issue_filter,omitempty"`
}

type MonitorIssueDefaultFilter struct {
	Tags    []string `yaml:"tags,omitempty"`
	TagMode string   `yaml:"tag_mode,omitempty"`
}

// PSConfig holds defaults for the orch ps command.
type PSConfig struct {
	DefaultStatuses []string `yaml:"default_statuses,omitempty"`
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
	DefaultModel     string   `yaml:"default_model,omitempty"`
	DefaultVariant   string   `yaml:"default_variant,omitempty"`
	PromptTemplate   string   `yaml:"prompt_template,omitempty"`
	ExtraArgs        []string `yaml:"extra_args,omitempty"`         // Additional CLI args for run agents
	ControlExtraArgs []string `yaml:"control_extra_args,omitempty"` // Additional CLI args for control agent
}

// ClaudeConfig holds default configuration for the claude agent.
type ClaudeConfig struct {
	PromptTemplate   string   `yaml:"prompt_template,omitempty"`
	ExtraArgs        []string `yaml:"extra_args,omitempty"`         // Additional CLI args for run agents
	ControlExtraArgs []string `yaml:"control_extra_args,omitempty"` // Additional CLI args for control agent
}

// CodexProfile binds a codex "account" to an execution target and optional
// CODEX_HOME auth directory. Profiles are selectable per run (--codex-profile)
// and defaulted per project (codex.default_profile).
type CodexProfile struct {
	Target    string `yaml:"target,omitempty"`     // config.targets name to run on (e.g. "mac"); empty = master/local
	CodexHome string `yaml:"codex_home,omitempty"` // CODEX_HOME for this profile; empty = agent default (~/.codex)
	// AllowedTargets restricts which config.targets names this profile may run
	// on (e.g. ["mac"]). These are target NAMES (matched against Target and
	// `orch run --on`), not resolved hostnames. The local/master target is
	// matched as "local". Empty list = any target allowed.
	AllowedTargets []string `yaml:"allowed_targets,omitempty"`
}

// CodexConfig holds default configuration for the codex agent.
type CodexConfig struct {
	DefaultModel     string   `yaml:"default_model,omitempty"`
	DefaultVariant   string   `yaml:"default_variant,omitempty"` // Reasoning effort (low/medium/high/xhigh); passed via `-c model_reasoning_effort`
	PromptTemplate   string   `yaml:"prompt_template,omitempty"`
	ExtraArgs        []string `yaml:"extra_args,omitempty"`         // Additional CLI args for run agents
	ControlExtraArgs []string `yaml:"control_extra_args,omitempty"` // Additional CLI args for control agent

	// DefaultProfile is the codex profile selected when --codex-profile is not
	// provided for a run in this project.
	DefaultProfile string `yaml:"default_profile,omitempty"`
	// Profiles maps a profile name (e.g. "company"/"personal") to its execution
	// binding (target + CODEX_HOME + allowed targets).
	Profiles map[string]CodexProfile `yaml:"profiles,omitempty"`
}

// GeminiConfig holds default configuration for the gemini agent.
type GeminiConfig struct {
	PromptTemplate   string   `yaml:"prompt_template,omitempty"`
	ExtraArgs        []string `yaml:"extra_args,omitempty"`         // Additional CLI args for run agents
	ControlExtraArgs []string `yaml:"control_extra_args,omitempty"` // Additional CLI args for control agent
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

type TargetConfig struct {
	Name string `yaml:"name"`
	Host string `yaml:"host"`
}

func (s *SlackConfig) ShouldNotify(status string) bool {
	if !s.Enabled {
		return false
	}
	if len(s.NotifyOn) == 0 {
		return status == "waiting" || status == "blocked" || status == "rate_limited" || status == "blocked_api"
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
	Agent              string           `yaml:"agent"`
	Model              string           `yaml:"model"`
	ModelVariant       string           `yaml:"model_variant"`
	WorktreeDir        string           `yaml:"worktree_dir"`
	BaseBranch         string           `yaml:"base_branch"`
	PRTargetBranch     string           `yaml:"pr_target_branch"`
	LogLevel           string           `yaml:"log_level"`
	PromptTemplate     string           `yaml:"prompt_template"`
	Multiplexer        string           `yaml:"multiplexer"`         // Deprecated: use MonitorMultiplexer/AgentMultiplexer
	MonitorMultiplexer string           `yaml:"monitor_multiplexer"` // Multiplexer for orch-monitor: "zellij" (default) or "tmux"
	AgentMultiplexer   string           `yaml:"agent_multiplexer"`   // Multiplexer for agent sessions: "tmux" (default) or "zellij"
	NoPR               bool             `yaml:"no_pr"`
	PS                 PSConfig         `yaml:"ps"`
	Monitor            MonitorConfig    `yaml:"monitor"`
	Presets            []Preset         `yaml:"presets"`
	OpenCodePresets    []OpenCodePreset `yaml:"opencode_presets"` // Deprecated: use Presets instead
	OpenCode           OpenCodeConfig   `yaml:"opencode"`
	Claude             ClaudeConfig     `yaml:"claude"`
	Codex              CodexConfig      `yaml:"codex"`
	Gemini             GeminiConfig     `yaml:"gemini"`
	DefaultPreset      string           `yaml:"default_preset"` // Default preset to use when no --preset flag is provided
	Slack              SlackConfig      `yaml:"slack"`
	Issues             IssuesConfig     `yaml:"issues"`
	GitHub             GitHubConfig     `yaml:"github"`
	Targets            []TargetConfig   `yaml:"targets,omitempty"`

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
	PS                  PSConfig         `yaml:"ps"`
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
	Targets             []TargetConfig   `yaml:"targets"`
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

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func LoadFromProjectRoot(projectRoot string) (*Config, error) {
	cfg := &Config{}

	globalPath := globalConfigPath()
	if globalPath != "" {
		if err := loadFromFile(globalPath, cfg); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}

	applyEnv(cfg)

	configPath := filepath.Join(projectRoot, ".orch", configFile)
	if _, err := os.Stat(configPath); err == nil {
		if err := loadFromFile(configPath, cfg); err != nil {
			return nil, err
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
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
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fileCfg); err != nil {
		return fmt.Errorf("invalid config schema in %s: %w", path, err)
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
	if len(fileCfg.PS.DefaultStatuses) > 0 {
		cfg.PS.DefaultStatuses = fileCfg.PS.DefaultStatuses
	}
	if len(fileCfg.Monitor.PSColumns) > 0 {
		cfg.Monitor.PSColumns = fileCfg.Monitor.PSColumns
	}
	if len(fileCfg.Monitor.DefaultRunStatuses) > 0 {
		cfg.Monitor.DefaultRunStatuses = fileCfg.Monitor.DefaultRunStatuses
	}
	if len(fileCfg.Monitor.DefaultIssueStatuses) > 0 {
		cfg.Monitor.DefaultIssueStatuses = fileCfg.Monitor.DefaultIssueStatuses
	}
	if len(fileCfg.Monitor.DefaultIssueFilter.Tags) > 0 || fileCfg.Monitor.DefaultIssueFilter.TagMode != "" {
		cfg.Monitor.DefaultIssueFilter = fileCfg.Monitor.DefaultIssueFilter
	}
	if len(fileCfg.Presets) > 0 {
		cfg.Presets = fileCfg.Presets
	}
	if len(fileCfg.Targets) > 0 {
		cfg.Targets = fileCfg.Targets
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
		if len(fileCfg.OpenCode.ExtraArgs) > 0 {
			cfg.OpenCode.ExtraArgs = fileCfg.OpenCode.ExtraArgs
		}
		if len(fileCfg.OpenCode.ControlExtraArgs) > 0 {
			cfg.OpenCode.ControlExtraArgs = fileCfg.OpenCode.ControlExtraArgs
		}
	}
	if fileCfg.Claude != nil {
		if fileCfg.Claude.PromptTemplate != "" {
			cfg.Claude.PromptTemplate = fileCfg.Claude.PromptTemplate
		}
		if len(fileCfg.Claude.ExtraArgs) > 0 {
			cfg.Claude.ExtraArgs = fileCfg.Claude.ExtraArgs
		}
		if len(fileCfg.Claude.ControlExtraArgs) > 0 {
			cfg.Claude.ControlExtraArgs = fileCfg.Claude.ControlExtraArgs
		}
	}
	if fileCfg.Codex != nil {
		if fileCfg.Codex.DefaultModel != "" {
			cfg.Codex.DefaultModel = fileCfg.Codex.DefaultModel
		}
		if fileCfg.Codex.PromptTemplate != "" {
			cfg.Codex.PromptTemplate = fileCfg.Codex.PromptTemplate
		}
		if len(fileCfg.Codex.ExtraArgs) > 0 {
			cfg.Codex.ExtraArgs = fileCfg.Codex.ExtraArgs
		}
		if len(fileCfg.Codex.ControlExtraArgs) > 0 {
			cfg.Codex.ControlExtraArgs = fileCfg.Codex.ControlExtraArgs
		}
		if fileCfg.Codex.DefaultProfile != "" {
			cfg.Codex.DefaultProfile = fileCfg.Codex.DefaultProfile
		}
		if len(fileCfg.Codex.Profiles) > 0 {
			cfg.Codex.Profiles = fileCfg.Codex.Profiles
		}
	}
	if fileCfg.Gemini != nil {
		if fileCfg.Gemini.PromptTemplate != "" {
			cfg.Gemini.PromptTemplate = fileCfg.Gemini.PromptTemplate
		}
		if len(fileCfg.Gemini.ExtraArgs) > 0 {
			cfg.Gemini.ExtraArgs = fileCfg.Gemini.ExtraArgs
		}
		if len(fileCfg.Gemini.ControlExtraArgs) > 0 {
			cfg.Gemini.ControlExtraArgs = fileCfg.Gemini.ControlExtraArgs
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
	if fileCfg.DiffTool != "" {
		cfg.DiffTool = fileCfg.DiffTool
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

// GetProjectRoot returns the project root directory (where .orch/ lives).
// It searches for .orch/config.yaml in the current directory and parent directories.
// Returns the directory containing .orch/, or an error if not found.
//
// Precedence:
// 1. Directory containing .orch/config.yaml (searched upward from cwd)
func GetProjectRoot() (string, error) {
	configPath, err := findRepoConfig()
	if err != nil {
		return "", err
	}
	if configPath != "" {
		orchDir := filepath.Dir(configPath)
		return filepath.Dir(orchDir), nil
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
	if v := os.Getenv("ORCH_CODEX_DEFAULT_MODEL"); v != "" {
		cfg.Codex.DefaultModel = v
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

func (c *Config) GetTarget(name string) *TargetConfig {
	for i := range c.Targets {
		if c.Targets[i].Name == name {
			return &c.Targets[i]
		}
	}
	return nil
}

// GetCodexProfile returns the codex execution profile with the given name.
// The bool result reports whether a profile with that name is configured;
// callers must fail fast on a false result rather than silently defaulting.
func (c *Config) GetCodexProfile(name string) (CodexProfile, bool) {
	p, ok := c.Codex.Profiles[name]
	return p, ok
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

var validIssueBackends = map[string]bool{
	"local":  true,
	"github": true,
}

var validLegacyMultiplexers = map[string]bool{
	"auto":   true,
	"tmux":   true,
	"zellij": true,
}

var validMultiplexers = map[string]bool{
	"tmux":   true,
	"zellij": true,
}

var validLogLevels = map[string]bool{
	"error": true,
	"warn":  true,
	"info":  true,
	"debug": true,
}

var validSlackNotifyStatuses = map[string]bool{
	"waiting":      true,
	"blocked":      true,
	"rate_limited": true,
	"blocked_api":  true,
	"done":         true,
	"failed":       true,
}

// Validate enforces config key/value constraints after loading/merging.
func (c *Config) Validate() error {
	var errs []string

	if c.Agent != "" && !validBackends[c.Agent] {
		errs = append(errs, fmt.Sprintf("agent must be one of %s (got %q)", joinAllowedKeys(validBackends), c.Agent))
	}
	if c.ControlAgent != "" && !validBackends[c.ControlAgent] {
		errs = append(errs, fmt.Sprintf("control_agent must be one of %s (got %q)", joinAllowedKeys(validBackends), c.ControlAgent))
	}
	if c.Issues.Backend != "" && !validIssueBackends[c.Issues.Backend] {
		errs = append(errs, fmt.Sprintf("issues.backend must be one of %s (got %q)", joinAllowedKeys(validIssueBackends), c.Issues.Backend))
	}
	if c.LogLevel != "" && !validLogLevels[c.LogLevel] {
		errs = append(errs, fmt.Sprintf("log_level must be one of %s (got %q)", joinAllowedKeys(validLogLevels), c.LogLevel))
	}
	if c.Multiplexer != "" && !validLegacyMultiplexers[c.Multiplexer] {
		errs = append(errs, fmt.Sprintf("multiplexer must be one of %s (got %q)", joinAllowedKeys(validLegacyMultiplexers), c.Multiplexer))
	}
	if c.MonitorMultiplexer != "" && !validMultiplexers[c.MonitorMultiplexer] {
		errs = append(errs, fmt.Sprintf("monitor_multiplexer must be one of %s (got %q)", joinAllowedKeys(validMultiplexers), c.MonitorMultiplexer))
	}
	if c.AgentMultiplexer != "" && !validMultiplexers[c.AgentMultiplexer] {
		errs = append(errs, fmt.Sprintf("agent_multiplexer must be one of %s (got %q)", joinAllowedKeys(validMultiplexers), c.AgentMultiplexer))
	}
	for _, status := range c.Slack.NotifyOn {
		if !validSlackNotifyStatuses[status] {
			errs = append(errs, fmt.Sprintf("slack.notify_on contains invalid status %q (allowed: %s)", status, joinAllowedKeys(validSlackNotifyStatuses)))
		}
	}
	for _, p := range c.Presets {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			errs = append(errs, "preset name must not be empty")
			continue
		}
		backend := p.EffectiveBackend()
		if !validBackends[backend] {
			errs = append(errs, fmt.Sprintf("preset %q has invalid backend %q (allowed: %s)", name, backend, joinAllowedKeys(validBackends)))
		}
	}
	for _, p := range c.OpenCodePresets {
		if strings.TrimSpace(p.Name) == "" {
			errs = append(errs, "opencode_preset name must not be empty")
		}
	}
	for _, target := range c.Targets {
		if strings.TrimSpace(target.Name) == "" {
			errs = append(errs, "target name must not be empty")
			continue
		}
		if strings.TrimSpace(target.Host) == "" {
			errs = append(errs, fmt.Sprintf("target %q host must not be empty", target.Name))
		}
	}
	if err := c.ValidateMultiplexerConfig(); err != nil {
		errs = append(errs, err.Error())
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid config: %s", strings.Join(errs, "; "))
	}
	return nil
}

func joinAllowedKeys(values map[string]bool) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
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

// ResolveModelAndVariant returns the effective model and variant for a given agent.
// Precedence: explicit request value > preset > agent-specific config > generic config.
func (c *Config) ResolveModelAndVariant(agent, preset, reqModel, reqVariant string) (string, string) {
	model := reqModel
	variant := reqVariant

	// Apply preset defaults for unset fields
	if preset != "" {
		if p := c.GetPreset(preset); p != nil {
			if model == "" {
				model = p.Model
			}
			if variant == "" {
				variant = p.Variant
			}
		}
	}

	// Agent-specific config defaults
	if model == "" {
		switch agent {
		case "opencode":
			model = c.OpenCode.DefaultModel
		case "codex":
			model = c.Codex.DefaultModel
		}
	}
	if model == "" {
		model = c.Model
	}

	if variant == "" {
		switch agent {
		case "opencode":
			variant = c.OpenCode.DefaultVariant
		case "codex":
			variant = c.Codex.DefaultVariant
		}
	}
	if variant == "" {
		variant = c.ModelVariant
	}

	return model, variant
}

// ResolveControlModelAndVariant returns the effective model and variant for a
// control agent.  Precedence: ControlModel/ControlModelVariant > agent-specific
// config (e.g. opencode defaults) > generic config (Model/ModelVariant).
func (c *Config) ResolveControlModelAndVariant(agent string) (string, string) {
	model := c.ControlModel
	variant := c.ControlModelVariant

	// Agent-specific config defaults
	if model == "" {
		switch agent {
		case "opencode":
			model = c.OpenCode.DefaultModel
		case "codex":
			model = c.Codex.DefaultModel
		}
	}
	if model == "" {
		model = c.Model
	}

	if variant == "" {
		switch agent {
		case "opencode":
			variant = c.OpenCode.DefaultVariant
		case "codex":
			variant = c.Codex.DefaultVariant
		}
	}
	if variant == "" {
		variant = c.ModelVariant
	}

	return model, variant
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

// GetExtraArgs returns the extra CLI arguments for run agents of the given type.
// Returns nil if no extra args are configured.
func (c *Config) GetExtraArgs(agent string) []string {
	switch agent {
	case "opencode":
		return c.OpenCode.ExtraArgs
	case "claude":
		return c.Claude.ExtraArgs
	case "codex":
		return c.Codex.ExtraArgs
	case "gemini":
		return c.Gemini.ExtraArgs
	}
	return nil
}

// GetControlExtraArgs returns the extra CLI arguments for the control agent.
// Returns nil if no control-specific extra args are configured.
func (c *Config) GetControlExtraArgs(agent string) []string {
	switch agent {
	case "opencode":
		return c.OpenCode.ControlExtraArgs
	case "claude":
		return c.Claude.ControlExtraArgs
	case "codex":
		return c.Codex.ControlExtraArgs
	case "gemini":
		return c.Gemini.ControlExtraArgs
	}
	return nil
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

// ErrInvalidMultiplexerConfig is returned when monitor and agent multiplexer
// configuration is invalid (e.g., both set to zellij)
var ErrInvalidMultiplexerConfig = fmt.Errorf("invalid multiplexer configuration")

// ValidateMultiplexerConfig checks if the multiplexer configuration is valid.
// Returns an error if monitor=zellij and agent=zellij (cross-session attach doesn't work).
func (c *Config) ValidateMultiplexerConfig() error {
	monitorMux := c.GetMonitorMultiplexer()
	agentMux := c.GetAgentMultiplexer()

	if monitorMux == "zellij" && agentMux == "zellij" {
		return fmt.Errorf("%w: monitor_multiplexer=zellij with agent_multiplexer=zellij is not supported (cross-session Zellij attach doesn't work). Use agent_multiplexer=tmux instead", ErrInvalidMultiplexerConfig)
	}
	return nil
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
