//! Agent alive detection for tmux-based agents.
//!
//! This module provides functionality to detect if tmux-based agents
//! (claude, codex, gemini) are still alive and running.

use std::collections::{HashMap, HashSet};
use std::process::Command;
use std::time::{Duration, SystemTime};
use thiserror::Error;

#[derive(Error, Debug)]
pub enum AliveError {
    #[error("failed to execute tmux command: {0}")]
    TmuxCommandFailed(String),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
}

/// Result of alive check for a session.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AliveStatus {
    /// Agent is alive and running
    Alive,
    /// Agent is not alive (session doesn't exist or only shell is running)
    Dead,
    /// Unable to determine status
    Unknown,
}

impl std::fmt::Display for AliveStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            AliveStatus::Alive => write!(f, "yes"),
            AliveStatus::Dead => write!(f, "no"),
            AliveStatus::Unknown => write!(f, "unknown"),
        }
    }
}

/// Check if a tmux session exists.
pub fn is_tmux_session_alive(session: &str) -> bool {
    Command::new("tmux")
        .args(["has-session", "-t", session])
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false)
}

/// Get the PID of a tmux session's pane.
/// Returns None if the session doesn't exist or if the command fails.
pub fn get_tmux_session_pid(session: &str) -> Option<u32> {
    let output = Command::new("tmux")
        .args(["list-panes", "-t", session, "-F", "#{pane_pid}"])
        .output()
        .ok()?;

    if !output.status.success() {
        return None;
    }

    let stdout = String::from_utf8_lossy(&output.stdout);
    let first_line = stdout.lines().next()?;
    first_line.trim().parse().ok()
}

/// List of shell commands that indicate the agent is not running.
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

fn is_shell_command(command: &str) -> bool {
    let cmd = command.trim().to_lowercase();
    SHELL_COMMANDS.contains(&cmd.as_str())
}

/// Check if an agent is alive in a tmux session by examining running commands.
/// Returns (is_alive, is_known) where:
/// - is_alive: true if a non-shell process is running
/// - is_known: true if we could determine the status
fn check_agent_alive_from_commands(session: &str, pane_commands: &HashMap<String, Vec<String>>) -> (bool, bool) {
    if let Some(commands) = pane_commands.get(session) {
        if commands.is_empty() {
            return (false, true);
        }
        
        for cmd in commands {
            if !is_shell_command(cmd) {
                return (true, true);
            }
        }
        
        return (false, true);
    }
    
    (false, false)
}

/// Cache for tmux session data to improve batch operation performance.
pub struct TmuxCache {
    sessions: HashSet<String>,
    pane_commands: HashMap<String, Vec<String>>,
    last_refreshed: SystemTime,
    ttl: Duration,
}

impl TmuxCache {
    /// Create a new tmux cache with the given TTL.
    pub fn new(ttl: Duration) -> Self {
        Self {
            sessions: HashSet::new(),
            pane_commands: HashMap::new(),
            last_refreshed: SystemTime::UNIX_EPOCH,
            ttl,
        }
    }

    /// Create a new tmux cache with default TTL (5 seconds).
    pub fn default() -> Self {
        Self::new(Duration::from_secs(5))
    }

    /// Check if the cache is stale and needs refreshing.
    pub fn is_stale(&self) -> bool {
        SystemTime::now()
            .duration_since(self.last_refreshed)
            .map(|d| d >= self.ttl)
            .unwrap_or(true)
    }

    /// Refresh the cache with current tmux state.
    pub fn refresh(&mut self) -> Result<(), AliveError> {
        self.sessions.clear();
        if let Ok(sessions) = list_all_sessions() {
            self.sessions.extend(sessions);
        }

        self.pane_commands.clear();
        if let Ok(commands) = list_all_pane_commands() {
            self.pane_commands = commands;
        }

        self.last_refreshed = SystemTime::now();
        Ok(())
    }

    /// Ensure the cache is fresh, refreshing if necessary.
    pub fn ensure_fresh(&mut self) -> Result<(), AliveError> {
        if self.is_stale() {
            self.refresh()?;
        }
        Ok(())
    }

    /// Check if a session is alive using cached data.
    pub fn is_session_alive(&self, session: &str) -> AliveStatus {
        if !self.sessions.contains(session) {
            return AliveStatus::Dead;
        }

        let (alive, known) = check_agent_alive_from_commands(session, &self.pane_commands);
        
        if !known {
            return AliveStatus::Alive;
        }

        if alive {
            AliveStatus::Alive
        } else {
            AliveStatus::Dead
        }
    }
}

/// List all tmux sessions.
fn list_all_sessions() -> Result<Vec<String>, AliveError> {
    let output = Command::new("tmux")
        .args(["list-sessions", "-F", "#{session_name}"])
        .output()?;

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        if stderr.contains("no server running") {
            return Ok(Vec::new());
        }
        return Err(AliveError::TmuxCommandFailed(stderr.to_string()));
    }

    let stdout = String::from_utf8_lossy(&output.stdout);
    Ok(stdout
        .lines()
        .filter(|s| !s.is_empty())
        .map(|s| s.to_string())
        .collect())
}

/// List all pane commands grouped by session.
fn list_all_pane_commands() -> Result<HashMap<String, Vec<String>>, AliveError> {
    let output = Command::new("tmux")
        .args(["list-panes", "-a", "-F", "#{session_name}\t#{pane_current_command}"])
        .output()?;

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        if stderr.contains("no server running") {
            return Ok(HashMap::new());
        }
        return Err(AliveError::TmuxCommandFailed(stderr.to_string()));
    }

    let stdout = String::from_utf8_lossy(&output.stdout);
    let mut commands: HashMap<String, Vec<String>> = HashMap::new();

    for line in stdout.lines() {
        if line.is_empty() {
            continue;
        }

        let parts: Vec<&str> = line.splitn(2, '\t').collect();
        if parts.len() != 2 {
            continue;
        }

        let session = parts[0].trim();
        let command = parts[1].trim();

        if session.is_empty() {
            continue;
        }

        commands
            .entry(session.to_string())
            .or_insert_with(Vec::new)
            .push(command.to_string());
    }

    Ok(commands)
}

/// Check if a tmux session is alive (session exists and has non-shell process).
pub fn check_tmux_alive(session: &str) -> AliveStatus {
    if !is_tmux_session_alive(session) {
        return AliveStatus::Dead;
    }

    match list_all_pane_commands() {
        Ok(pane_commands) => {
            let (alive, known) = check_agent_alive_from_commands(session, &pane_commands);
            
            if !known {
                return AliveStatus::Alive;
            }

            if alive {
                AliveStatus::Alive
            } else {
                AliveStatus::Dead
            }
        }
        Err(_) => AliveStatus::Alive,
    }
}

/// Check alive status for multiple sessions efficiently using a cache.
pub fn check_alive_batch(sessions: &[String]) -> HashMap<String, AliveStatus> {
    let mut cache = TmuxCache::default();
    
    if let Err(_) = cache.refresh() {
        return sessions
            .iter()
            .map(|s| (s.clone(), AliveStatus::Unknown))
            .collect();
    }

    sessions
        .iter()
        .map(|session| (session.clone(), cache.is_session_alive(session)))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_is_shell_command() {
        assert!(is_shell_command("bash"));
        assert!(is_shell_command("zsh"));
        assert!(is_shell_command("BASH"));
        assert!(!is_shell_command("claude"));
        assert!(!is_shell_command("opencode"));
        assert!(!is_shell_command(""));
    }

    #[test]
    fn test_alive_status_display() {
        assert_eq!(AliveStatus::Alive.to_string(), "yes");
        assert_eq!(AliveStatus::Dead.to_string(), "no");
        assert_eq!(AliveStatus::Unknown.to_string(), "unknown");
    }

    #[test]
    fn test_tmux_cache_is_stale() {
        let cache = TmuxCache::new(Duration::from_millis(100));
        assert!(cache.is_stale());

        let mut cache = TmuxCache::default();
        cache.last_refreshed = SystemTime::now();
        assert!(!cache.is_stale());
    }
}
