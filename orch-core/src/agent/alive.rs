//! Agent alive detection functionality.
//!
//! This module provides functions to detect if agents are alive by checking:
//! - Tmux session existence and active processes
//! - OpenCode HTTP health endpoints
//! - Batch operations for efficient checking of multiple runs

use std::collections::{HashMap, HashSet};
use std::process::Command;
use std::time::{Duration, SystemTime};

use crate::agent::opencode;
use crate::models::Run;
use crate::tmux;

/// Status of whether an agent is alive.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AliveStatus {
    /// Agent is alive and running
    Alive,
    /// Agent is not alive
    Dead,
    /// Unable to determine status
    Unknown,
}

impl std::fmt::Display for AliveStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Alive => write!(f, "yes"),
            Self::Dead => write!(f, "no"),
            Self::Unknown => write!(f, "?"),
        }
    }
}

/// Cache for tmux session data to enable efficient batch operations.
pub struct TmuxCache {
    sessions: HashSet<String>,
    pane_commands: HashMap<String, Vec<String>>,
    last_refreshed: SystemTime,
    ttl: Duration,
}

impl TmuxCache {
    /// Create a new tmux cache with default TTL of 5 seconds.
    pub fn new() -> Self {
        Self {
            sessions: HashSet::new(),
            pane_commands: HashMap::new(),
            last_refreshed: SystemTime::UNIX_EPOCH,
            ttl: Duration::from_secs(5),
        }
    }

    /// Check if the cache is stale and needs refreshing.
    pub fn is_stale(&self) -> bool {
        SystemTime::now()
            .duration_since(self.last_refreshed)
            .map(|d| d > self.ttl)
            .unwrap_or(true)
    }

    /// Refresh the cache with current tmux state.
    pub fn refresh(&mut self) {
        if let Ok(sessions) = tmux::list_sessions() {
            self.sessions = sessions.into_iter().collect();
        }

        if let Ok(commands) = list_pane_commands() {
            self.pane_commands = commands;
        }

        self.last_refreshed = SystemTime::now();
    }

    /// Get cached session existence.
    pub fn has_session(&mut self, session: &str) -> Option<bool> {
        if self.is_stale() {
            self.refresh();
        }
        Some(self.sessions.contains(session))
    }

    /// Get cached pane commands for a session.
    pub fn get_pane_commands(&mut self, session: &str) -> Option<&Vec<String>> {
        if self.is_stale() {
            self.refresh();
        }
        self.pane_commands.get(session)
    }
}

impl Default for TmuxCache {
    fn default() -> Self {
        Self::new()
    }
}

/// Check if a tmux session is alive (exists and has active processes).
pub fn is_tmux_session_alive(session: &str) -> bool {
    if !tmux::session_exists(session) {
        return false;
    }

    if let Ok(commands) = list_pane_commands() {
        if let Some(session_commands) = commands.get(session) {
            return has_non_shell_command(session_commands);
        }
    }

    true
}

/// Get the PID of a tmux session's pane.
pub fn get_tmux_session_pid(session: &str) -> Option<u32> {
    let output = Command::new("tmux")
        .args(["list-panes", "-t", session, "-F", "#{pane_pid}"])
        .output()
        .ok()?;

    if !output.status.success() {
        return None;
    }

    let stdout = String::from_utf8_lossy(&output.stdout);
    stdout.lines().next()?.trim().parse().ok()
}

/// List all pane commands grouped by session name.
pub fn list_pane_commands() -> Result<HashMap<String, Vec<String>>, std::io::Error> {
    let output = Command::new("tmux")
        .args([
            "list-panes",
            "-a",
            "-F",
            "#{session_name}\t#{pane_current_command}",
        ])
        .output()?;

    if !output.status.success() {
        if String::from_utf8_lossy(&output.stderr).contains("no server running") {
            return Ok(HashMap::new());
        }
        return Err(std::io::Error::new(
            std::io::ErrorKind::Other,
            "tmux command failed",
        ));
    }

    let stdout = String::from_utf8_lossy(&output.stdout);
    let mut commands: HashMap<String, Vec<String>> = HashMap::new();

    for line in stdout.lines() {
        if let Some((session, command)) = line.split_once('\t') {
            let session = session.trim();
            let command = command.trim();
            if !session.is_empty() {
                commands
                    .entry(session.to_string())
                    .or_default()
                    .push(command.to_string());
            }
        }
    }

    Ok(commands)
}

/// Check if any command in the list is not a shell command.
fn has_non_shell_command(commands: &[String]) -> bool {
    const SHELL_COMMANDS: &[&str] = &[
        "bash",
        "zsh",
        "sh",
        "fish",
        "ksh",
        "tcsh",
        "dash",
        "pwsh",
        "powershell",
        "cmd",
        "cmd.exe",
        "nu",
        "elvish",
    ];

    for command in commands {
        let command = command.trim().to_lowercase();
        if !command.is_empty() && !SHELL_COMMANDS.contains(&command.as_str()) {
            return true;
        }
    }

    false
}

/// Check if a run is alive using cached data for batch operations.
pub fn is_run_alive_cached(run: &Run, cache: &mut TmuxCache) -> AliveStatus {
    if run.agent == "opencode" {
        if run.server_port > 0 && !run.opencode_session_id.is_empty() {
            let alive = opencode::is_opencode_session_alive(
                run.server_port,
                &run.opencode_session_id,
            );
            return if alive {
                AliveStatus::Alive
            } else {
                AliveStatus::Dead
            };
        }
        return AliveStatus::Unknown;
    }

    let session = if !run.tmux_session.is_empty() {
        &run.tmux_session
    } else {
        return AliveStatus::Unknown;
    };

    match cache.has_session(session) {
        Some(false) => return AliveStatus::Dead,
        Some(true) => {
            if let Some(commands) = cache.get_pane_commands(session) {
                if has_non_shell_command(commands) {
                    return AliveStatus::Alive;
                }
                return AliveStatus::Dead;
            }
            AliveStatus::Alive
        }
        None => AliveStatus::Unknown,
    }
}

/// Check alive status for multiple runs in batch (efficient for large lists).
pub fn check_alive_batch(runs: &[Run]) -> HashMap<String, AliveStatus> {
    let mut cache = TmuxCache::new();
    let mut result = HashMap::new();

    cache.refresh();

    for run in runs {
        let run_ref = format!("{}#{}", run.issue_id, run.run_id);
        let status = is_run_alive_cached(run, &mut cache);
        result.insert(run_ref, status);
    }

    result
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_has_non_shell_command() {
        assert!(!has_non_shell_command(&[]));
        assert!(!has_non_shell_command(&["bash".to_string()]));
        assert!(!has_non_shell_command(&["zsh".to_string(), "sh".to_string()]));
        assert!(has_non_shell_command(&["claude".to_string()]));
        assert!(has_non_shell_command(&["bash".to_string(), "codex".to_string()]));
    }

    #[test]
    fn test_alive_status_display() {
        assert_eq!(AliveStatus::Alive.to_string(), "yes");
        assert_eq!(AliveStatus::Dead.to_string(), "no");
        assert_eq!(AliveStatus::Unknown.to_string(), "?");
    }
}
