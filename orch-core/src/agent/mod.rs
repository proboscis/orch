//! Agent adapters for different LLM CLIs.
//!
//! This module provides adapters for launching and interacting with
//! different LLM agents (claude, codex, gemini, opencode).
//! This will be fully implemented in Phase 2/3.

pub mod alive;
pub mod opencode;

use thiserror::Error;

#[derive(Error, Debug)]
pub enum AgentError {
    #[error("agent not found: {0}")]
    NotFound(String),

    #[error("agent failed to start: {0}")]
    StartFailed(String),

    #[error("agent health check failed: {0}")]
    HealthCheckFailed(String),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
}

/// Supported agent types.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AgentType {
    Claude,
    Codex,
    Gemini,
    OpenCode,
    Custom,
}

impl std::str::FromStr for AgentType {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_lowercase().as_str() {
            "claude" => Ok(Self::Claude),
            "codex" => Ok(Self::Codex),
            "gemini" => Ok(Self::Gemini),
            "opencode" => Ok(Self::OpenCode),
            "custom" => Ok(Self::Custom),
            _ => Err(format!("unknown agent type: {}", s)),
        }
    }
}

impl std::fmt::Display for AgentType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let s = match self {
            Self::Claude => "claude",
            Self::Codex => "codex",
            Self::Gemini => "gemini",
            Self::OpenCode => "opencode",
            Self::Custom => "custom",
        };
        write!(f, "{}", s)
    }
}

/// Get the command to launch an agent.
pub fn get_agent_command(agent_type: AgentType) -> Vec<String> {
    match agent_type {
        AgentType::Claude => vec!["claude".to_string()],
        AgentType::Codex => vec!["codex".to_string()],
        AgentType::Gemini => vec!["gemini".to_string()],
        AgentType::OpenCode => vec!["opencode".to_string()],
        AgentType::Custom => vec![],
    }
}

/// Check if an agent CLI is available.
pub fn is_agent_available(agent_type: AgentType) -> bool {
    let cmd = match agent_type {
        AgentType::Claude => "claude",
        AgentType::Codex => "codex",
        AgentType::Gemini => "gemini",
        AgentType::OpenCode => "opencode",
        AgentType::Custom => return true,
    };

    which::which(cmd).is_ok()
}

/// Check if a run is alive based on its agent type and configuration.
pub fn is_run_alive(
    agent_type: AgentType,
    tmux_session: Option<&str>,
    server_port: Option<u16>,
    opencode_session_id: Option<&str>,
    worktree_path: Option<&str>,
) -> alive::AliveStatus {
    match agent_type {
        AgentType::OpenCode => {
            if let (Some(port), Some(session_id)) = (server_port, opencode_session_id) {
                if opencode::is_session_alive(port, session_id, worktree_path) {
                    alive::AliveStatus::Alive
                } else {
                    alive::AliveStatus::Dead
                }
            } else {
                alive::AliveStatus::Unknown
            }
        }
        AgentType::Claude | AgentType::Codex | AgentType::Gemini | AgentType::Custom => {
            if let Some(session) = tmux_session {
                alive::check_tmux_alive(session)
            } else {
                alive::AliveStatus::Unknown
            }
        }
    }
}

use crate::models::Run;
use std::collections::HashMap;

/// Check alive status for multiple runs efficiently using batch operations.
pub fn check_runs_alive_batch(runs: &[Run]) -> HashMap<String, alive::AliveStatus> {
    let mut results = HashMap::new();
    
    let tmux_sessions: Vec<String> = runs
        .iter()
        .filter(|r| {
            let agent_type = r.agent.parse::<AgentType>().unwrap_or(AgentType::Custom);
            !matches!(agent_type, AgentType::OpenCode)
        })
        .filter_map(|r| {
            if !r.tmux_session.is_empty() {
                Some(r.tmux_session.clone())
            } else {
                None
            }
        })
        .collect();
    
    let tmux_results = if !tmux_sessions.is_empty() {
        alive::check_alive_batch(&tmux_sessions)
    } else {
        HashMap::new()
    };
    
    for run in runs {
        let run_ref = format!("{}#{}", run.issue_id, run.run_id);
        let agent_type = run.agent.parse::<AgentType>().unwrap_or(AgentType::Custom);
        
        let status = match agent_type {
            AgentType::OpenCode => {
                if run.server_port > 0 && !run.opencode_session_id.is_empty() {
                    let worktree = if run.worktree_path.is_empty() {
                        None
                    } else {
                        Some(run.worktree_path.as_str())
                    };
                    
                    if opencode::is_session_alive(
                        run.server_port as u16,
                        &run.opencode_session_id,
                        worktree,
                    ) {
                        alive::AliveStatus::Alive
                    } else {
                        alive::AliveStatus::Dead
                    }
                } else {
                    alive::AliveStatus::Unknown
                }
            }
            _ => {
                if !run.tmux_session.is_empty() {
                    *tmux_results
                        .get(&run.tmux_session)
                        .unwrap_or(&alive::AliveStatus::Unknown)
                } else {
                    alive::AliveStatus::Unknown
                }
            }
        };
        
        results.insert(run_ref, status);
    }
    
    results
}
