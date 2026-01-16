use std::collections::{HashMap, HashSet};
use std::process::Command;
use std::sync::{RwLock, OnceLock};
use std::time::{Duration, Instant};

use crate::models::Run;

static TMUX_CACHE: OnceLock<RwLock<TmuxCache>> = OnceLock::new();
const CACHE_TTL: Duration = Duration::from_secs(5);

static SHELL_COMMANDS: OnceLock<HashSet<&'static str>> = OnceLock::new();

fn shell_commands() -> &'static HashSet<&'static str> {
    SHELL_COMMANDS.get_or_init(|| {
        [
            "bash", "zsh", "sh", "fish", "ksh", "tcsh", 
            "dash", "pwsh", "powershell", "cmd", "cmd.exe", "nu", "elvish",
        ].into_iter().collect()
    })
}

struct TmuxCache {
    sessions: HashMap<String, bool>,
    pane_commands: HashMap<String, Vec<String>>,
    last_refreshed: Instant,
}

impl TmuxCache {
    fn new() -> Self {
        Self {
            sessions: HashMap::new(),
            pane_commands: HashMap::new(),
            last_refreshed: Instant::now() - CACHE_TTL - Duration::from_secs(1),
        }
    }

    fn is_stale(&self) -> bool {
        self.last_refreshed.elapsed() >= CACHE_TTL
    }
}

fn get_tmux_cache() -> &'static RwLock<TmuxCache> {
    TMUX_CACHE.get_or_init(|| RwLock::new(TmuxCache::new()))
}

pub fn refresh_tmux_cache() {
    let cache = get_tmux_cache();
    let mut cache_guard = cache.write().unwrap();

    if let Ok(sessions) = list_all_sessions() {
        cache_guard.sessions = sessions.into_iter().map(|s| (s, true)).collect();
    }

    if let Ok(pane_commands) = list_pane_commands() {
        cache_guard.pane_commands = pane_commands;
    }

    cache_guard.last_refreshed = Instant::now();
}

pub fn invalidate_tmux_cache() {
    let cache = get_tmux_cache();
    let mut cache_guard = cache.write().unwrap();
    cache_guard.last_refreshed = Instant::now() - CACHE_TTL - Duration::from_secs(1);
}

fn ensure_cache_fresh() {
    let cache = get_tmux_cache();
    let needs_refresh = {
        let cache_guard = cache.read().unwrap();
        cache_guard.is_stale()
    };

    if needs_refresh {
        refresh_tmux_cache();
    }
}

pub fn is_tmux_session_alive(session: &str) -> bool {
    ensure_cache_fresh();
    
    let cache = get_tmux_cache();
    let cache_guard = cache.read().unwrap();

    if !cache_guard.sessions.contains_key(session) {
        return false;
    }

    if let Some(commands) = cache_guard.pane_commands.get(session) {
        return commands.iter().any(|cmd| !is_shell_command(cmd));
    }

    true
}

pub fn get_tmux_session_pid(session: &str) -> Option<u32> {
    let output = Command::new("tmux")
        .args(["list-panes", "-t", session, "-F", "#{pane_pid}"])
        .output()
        .ok()?;

    if !output.status.success() {
        return None;
    }

    String::from_utf8_lossy(&output.stdout)
        .lines()
        .next()
        .and_then(|line| line.trim().parse().ok())
}

fn list_all_sessions() -> Result<Vec<String>, std::io::Error> {
    let output = Command::new("tmux")
        .args(["list-sessions", "-F", "#{session_name}"])
        .output()?;

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        if stderr.contains("no server running") {
            return Ok(Vec::new());
        }
        return Ok(Vec::new());
    }

    Ok(String::from_utf8_lossy(&output.stdout)
        .lines()
        .filter(|s| !s.is_empty())
        .map(|s| s.to_string())
        .collect())
}

fn list_pane_commands() -> Result<HashMap<String, Vec<String>>, std::io::Error> {
    let output = Command::new("tmux")
        .args(["list-panes", "-a", "-F", "#{session_name}\t#{pane_current_command}"])
        .output()?;

    if !output.status.success() {
        return Ok(HashMap::new());
    }

    let mut commands: HashMap<String, Vec<String>> = HashMap::new();

    for line in String::from_utf8_lossy(&output.stdout).lines() {
        if let Some((session, command)) = line.split_once('\t') {
            let session = session.trim();
            let command = command.trim();
            if !session.is_empty() {
                commands.entry(session.to_string())
                    .or_default()
                    .push(command.to_string());
            }
        }
    }

    Ok(commands)
}

fn is_shell_command(command: &str) -> bool {
    let command = command.to_lowercase();
    let command = command.trim();
    if command.is_empty() {
        return true;
    }
    shell_commands().contains(command)
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AliveStatus {
    Alive,
    Dead,
    Unknown,
}

impl std::fmt::Display for AliveStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            AliveStatus::Alive => write!(f, "yes"),
            AliveStatus::Dead => write!(f, "no"),
            AliveStatus::Unknown => write!(f, "?"),
        }
    }
}

pub fn check_alive_batch(runs: &[Run]) -> HashMap<String, AliveStatus> {
    ensure_cache_fresh();
    
    let mut results = HashMap::new();

    let opencode_runs: Vec<&Run> = runs.iter()
        .filter(|r| r.agent.to_lowercase() == "opencode")
        .collect();

    let tmux_runs: Vec<&Run> = runs.iter()
        .filter(|r| r.agent.to_lowercase() != "opencode")
        .collect();

    for run in tmux_runs {
        let ref_key = format!("{}#{}", run.issue_id, run.run_id);
        let session_name = if !run.tmux_session.is_empty() {
            run.tmux_session.clone()
        } else {
            crate::models::run::generate_tmux_session(&run.issue_id, &run.run_id)
        };

        let status = if is_tmux_session_alive(&session_name) {
            AliveStatus::Alive
        } else {
            AliveStatus::Dead
        };

        results.insert(ref_key, status);
    }

    if !opencode_runs.is_empty() {
        let opencode_statuses = super::opencode::check_opencode_batch(&opencode_runs);
        results.extend(opencode_statuses);
    }

    results
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_is_shell_command() {
        assert!(is_shell_command("bash"));
        assert!(is_shell_command("zsh"));
        assert!(is_shell_command("ZSH"));
        assert!(is_shell_command("  bash  "));
        assert!(!is_shell_command("claude"));
        assert!(!is_shell_command("opencode"));
        assert!(!is_shell_command("python"));
    }

    #[test]
    fn test_alive_status_display() {
        assert_eq!(format!("{}", AliveStatus::Alive), "yes");
        assert_eq!(format!("{}", AliveStatus::Dead), "no");
        assert_eq!(format!("{}", AliveStatus::Unknown), "?");
    }
}
