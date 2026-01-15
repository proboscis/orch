//! Agent adapters for different LLM CLIs.
//!
//! This module provides adapters for launching and interacting with
//! different LLM agents (claude, codex, gemini, opencode).
//! This will be fully implemented in Phase 2/3.

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
