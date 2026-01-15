//! Tmux operations for orch.
//!
//! This module provides tmux session management functionality.

use std::process::Command;
use thiserror::Error;

#[derive(Error, Debug)]
pub enum TmuxError {
    #[error("tmux command failed: {0}")]
    CommandFailed(String),

    #[error("session not found: {0}")]
    SessionNotFound(String),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
}

/// Check if a tmux session exists.
pub fn session_exists(session_name: &str) -> bool {
    Command::new("tmux")
        .args(["has-session", "-t", session_name])
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false)
}

/// Create a new tmux session.
pub fn create_session(session_name: &str, working_dir: Option<&str>) -> Result<(), TmuxError> {
    let mut args = vec!["new-session", "-d", "-s", session_name];
    
    if let Some(dir) = working_dir {
        args.extend(["-c", dir]);
    }

    let output = Command::new("tmux")
        .args(&args)
        .output()?;

    if !output.status.success() {
        return Err(TmuxError::CommandFailed(
            String::from_utf8_lossy(&output.stderr).to_string(),
        ));
    }

    Ok(())
}

/// Kill a tmux session.
pub fn kill_session(session_name: &str) -> Result<(), TmuxError> {
    let output = Command::new("tmux")
        .args(["kill-session", "-t", session_name])
        .output()?;

    if !output.status.success() {
        return Err(TmuxError::CommandFailed(
            String::from_utf8_lossy(&output.stderr).to_string(),
        ));
    }

    Ok(())
}

/// Send keys to a tmux session.
pub fn send_keys(session_name: &str, keys: &str, enter: bool) -> Result<(), TmuxError> {
    let mut args = vec!["send-keys", "-t", session_name, keys];
    
    if enter {
        args.push("Enter");
    }

    let output = Command::new("tmux")
        .args(&args)
        .output()?;

    if !output.status.success() {
        return Err(TmuxError::CommandFailed(
            String::from_utf8_lossy(&output.stderr).to_string(),
        ));
    }

    Ok(())
}

/// Capture the pane content from a tmux session.
pub fn capture_pane(session_name: &str) -> Result<String, TmuxError> {
    let output = Command::new("tmux")
        .args(["capture-pane", "-t", session_name, "-p"])
        .output()?;

    if !output.status.success() {
        return Err(TmuxError::CommandFailed(
            String::from_utf8_lossy(&output.stderr).to_string(),
        ));
    }

    Ok(String::from_utf8_lossy(&output.stdout).to_string())
}

/// List all tmux sessions.
pub fn list_sessions() -> Result<Vec<String>, TmuxError> {
    let output = Command::new("tmux")
        .args(["list-sessions", "-F", "#{session_name}"])
        .output()?;

    if !output.status.success() {
        // No sessions is not an error
        if String::from_utf8_lossy(&output.stderr).contains("no server running") {
            return Ok(Vec::new());
        }
        return Err(TmuxError::CommandFailed(
            String::from_utf8_lossy(&output.stderr).to_string(),
        ));
    }

    Ok(String::from_utf8_lossy(&output.stdout)
        .lines()
        .map(|s| s.to_string())
        .collect())
}

/// Check if tmux is available.
pub fn is_available() -> bool {
    Command::new("tmux")
        .arg("-V")
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false)
}
