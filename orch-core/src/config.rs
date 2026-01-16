//! Configuration management for orch.
//!
//! Loads configuration from `.orch/config.yaml` in the repository root,
//! with sensible defaults when the config file doesn't exist.

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

/// Default base branch for new worktrees.
pub const DEFAULT_BASE_BRANCH: &str = "origin/main";

/// Default directory name for worktrees (relative to repo root).
pub const DEFAULT_WORKTREE_ROOT: &str = ".git-worktrees";

/// Default agent to use for runs.
pub const DEFAULT_AGENT: &str = "claude";

/// Configuration for orch behavior.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(default)]
pub struct Config {
    /// Path to the vault directory (can be overridden by CLI or env).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub vault: Option<String>,

    /// Default agent to use for runs.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub agent: Option<String>,

    /// Directory for worktrees, relative to repo root.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub worktree_root: Option<String>,

    /// Base branch for creating new worktrees (e.g., "main", "origin/main").
    #[serde(skip_serializing_if = "Option::is_none")]
    pub base_branch: Option<String>,

    /// Target branch for PRs (defaults to base_branch if not set).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub pr_target_branch: Option<String>,
}

impl Config {
    /// Load config from `.orch/config.yaml` in the given directory.
    ///
    /// Returns default config if the file doesn't exist.
    pub fn load_from_dir(dir: &Path) -> Result<Self> {
        let config_path = dir.join(".orch").join("config.yaml");
        Self::load_from_path(&config_path)
    }

    /// Load config from the given path.
    ///
    /// Returns default config if the file doesn't exist.
    pub fn load_from_path(path: &Path) -> Result<Self> {
        if !path.exists() {
            return Ok(Self::default());
        }

        let content = std::fs::read_to_string(path)
            .with_context(|| format!("failed to read config: {}", path.display()))?;

        let config: Config = serde_yaml::from_str(&content)
            .with_context(|| format!("failed to parse config: {}", path.display()))?;

        Ok(config)
    }

    /// Get the base branch, with default fallback.
    pub fn base_branch(&self) -> &str {
        self.base_branch.as_deref().unwrap_or(DEFAULT_BASE_BRANCH)
    }

    /// Get the worktree root directory name, with default fallback.
    pub fn worktree_root(&self) -> &str {
        self.worktree_root.as_deref().unwrap_or(DEFAULT_WORKTREE_ROOT)
    }

    /// Get the default agent, with default fallback.
    pub fn agent(&self) -> &str {
        self.agent.as_deref().unwrap_or(DEFAULT_AGENT)
    }

    /// Get the PR target branch, falling back to base_branch (without origin/ prefix).
    pub fn pr_target_branch(&self) -> &str {
        if let Some(ref target) = self.pr_target_branch {
            return target;
        }
        // Strip "origin/" prefix from base_branch for PR target
        let base = self.base_branch();
        base.strip_prefix("origin/").unwrap_or(base)
    }

    /// Resolve the worktree root as an absolute path.
    pub fn worktree_root_path(&self, repo_root: &Path) -> PathBuf {
        repo_root.join(self.worktree_root())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    #[test]
    fn test_default_config() {
        let config = Config::default();
        assert_eq!(config.base_branch(), DEFAULT_BASE_BRANCH);
        assert_eq!(config.worktree_root(), DEFAULT_WORKTREE_ROOT);
        assert_eq!(config.agent(), DEFAULT_AGENT);
        assert_eq!(config.pr_target_branch(), "main"); // stripped origin/
    }

    #[test]
    fn test_load_missing_file() {
        let dir = TempDir::new().unwrap();
        let config = Config::load_from_dir(dir.path()).unwrap();
        assert_eq!(config.base_branch(), DEFAULT_BASE_BRANCH);
    }

    #[test]
    fn test_load_config_file() {
        let dir = TempDir::new().unwrap();
        let orch_dir = dir.path().join(".orch");
        std::fs::create_dir_all(&orch_dir).unwrap();

        let config_content = r#"
base_branch: develop
worktree_root: worktrees
agent: codex
pr_target_branch: staging
"#;
        std::fs::write(orch_dir.join("config.yaml"), config_content).unwrap();

        let config = Config::load_from_dir(dir.path()).unwrap();
        assert_eq!(config.base_branch(), "develop");
        assert_eq!(config.worktree_root(), "worktrees");
        assert_eq!(config.agent(), "codex");
        assert_eq!(config.pr_target_branch(), "staging");
    }

    #[test]
    fn test_partial_config() {
        let dir = TempDir::new().unwrap();
        let orch_dir = dir.path().join(".orch");
        std::fs::create_dir_all(&orch_dir).unwrap();

        // Only set base_branch, rest should use defaults
        let config_content = "base_branch: feature-branch\n";
        std::fs::write(orch_dir.join("config.yaml"), config_content).unwrap();

        let config = Config::load_from_dir(dir.path()).unwrap();
        assert_eq!(config.base_branch(), "feature-branch");
        assert_eq!(config.worktree_root(), DEFAULT_WORKTREE_ROOT);
        assert_eq!(config.agent(), DEFAULT_AGENT);
        assert_eq!(config.pr_target_branch(), "feature-branch"); // falls back to base_branch
    }

    #[test]
    fn test_worktree_root_path() {
        let config = Config {
            worktree_root: Some("my-worktrees".to_string()),
            ..Default::default()
        };
        let repo_root = Path::new("/home/user/repo");
        assert_eq!(
            config.worktree_root_path(repo_root),
            PathBuf::from("/home/user/repo/my-worktrees")
        );
    }
}
