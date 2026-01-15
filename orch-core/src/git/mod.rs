//! Git operations for orch.
//!
//! This module provides git-related functionality:
//! - Worktree management
//! - Branch operations
//! - Fetch and merge operations

use std::path::Path;
use std::process::Command;
use thiserror::Error;

#[derive(Error, Debug)]
pub enum GitError {
    #[error("not a git repository: {0}")]
    NotARepository(String),

    #[error("git command failed: {0}")]
    CommandFailed(String),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
}

/// Find the root of a git repository.
pub fn find_repo_root(path: impl AsRef<Path>) -> Result<std::path::PathBuf, GitError> {
    let output = Command::new("git")
        .args(["rev-parse", "--show-toplevel"])
        .current_dir(path.as_ref())
        .output()?;

    if !output.status.success() {
        return Err(GitError::NotARepository(
            path.as_ref().to_string_lossy().to_string(),
        ));
    }

    let root = String::from_utf8_lossy(&output.stdout)
        .trim()
        .to_string();
    Ok(std::path::PathBuf::from(root))
}

/// Fetch from a remote.
pub fn fetch(repo_path: impl AsRef<Path>, remote: &str) -> Result<(), GitError> {
    let remote = if remote.is_empty() { "origin" } else { remote };

    let output = Command::new("git")
        .args(["fetch", remote])
        .current_dir(repo_path.as_ref())
        .output()?;

    if !output.status.success() {
        return Err(GitError::CommandFailed(
            String::from_utf8_lossy(&output.stderr).to_string(),
        ));
    }

    Ok(())
}

/// Create a new branch from the current HEAD.
pub fn create_branch(repo_path: impl AsRef<Path>, branch_name: &str) -> Result<(), GitError> {
    let output = Command::new("git")
        .args(["checkout", "-b", branch_name])
        .current_dir(repo_path.as_ref())
        .output()?;

    if !output.status.success() {
        return Err(GitError::CommandFailed(
            String::from_utf8_lossy(&output.stderr).to_string(),
        ));
    }

    Ok(())
}

/// Create a git worktree.
pub fn create_worktree(
    repo_path: impl AsRef<Path>,
    worktree_path: impl AsRef<Path>,
    branch: &str,
) -> Result<(), GitError> {
    let output = Command::new("git")
        .args([
            "worktree",
            "add",
            worktree_path.as_ref().to_str().unwrap_or_default(),
            "-b",
            branch,
        ])
        .current_dir(repo_path.as_ref())
        .output()?;

    if !output.status.success() {
        return Err(GitError::CommandFailed(
            String::from_utf8_lossy(&output.stderr).to_string(),
        ));
    }

    Ok(())
}

/// Remove a git worktree.
pub fn remove_worktree(repo_path: impl AsRef<Path>, worktree_path: impl AsRef<Path>) -> Result<(), GitError> {
    let output = Command::new("git")
        .args([
            "worktree",
            "remove",
            worktree_path.as_ref().to_str().unwrap_or_default(),
            "--force",
        ])
        .current_dir(repo_path.as_ref())
        .output()?;

    if !output.status.success() {
        return Err(GitError::CommandFailed(
            String::from_utf8_lossy(&output.stderr).to_string(),
        ));
    }

    Ok(())
}

/// Get the current branch name.
pub fn current_branch(repo_path: impl AsRef<Path>) -> Result<String, GitError> {
    let output = Command::new("git")
        .args(["rev-parse", "--abbrev-ref", "HEAD"])
        .current_dir(repo_path.as_ref())
        .output()?;

    if !output.status.success() {
        return Err(GitError::CommandFailed(
            String::from_utf8_lossy(&output.stderr).to_string(),
        ));
    }

    Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
}
