//! OpenCode agent alive detection via HTTP health check.
//!
//! This module provides functionality to detect if OpenCode agents
//! are still alive by querying their HTTP API.

use serde::{Deserialize, Serialize};
use std::time::Duration;
use thiserror::Error;

#[derive(Error, Debug)]
pub enum OpenCodeError {
    #[error("HTTP request failed: {0}")]
    HttpError(String),

    #[error("server not running on port {0}")]
    ServerNotRunning(u16),

    #[error("session not found: {0}")]
    SessionNotFound(String),

    #[error("timeout waiting for response")]
    Timeout,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct OpenCodeSession {
    pub id: String,
    pub title: Option<String>,
    pub directory: Option<String>,
    #[serde(rename = "parentID")]
    pub parent_id: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum SessionStatus {
    Idle,
    Busy,
    Retry,
}

/// Check if the OpenCode server is running on the given port.
pub fn is_server_running(port: u16) -> bool {
    check_health(port, Duration::from_secs(2)).is_ok()
}

/// Perform a health check on the OpenCode server.
pub fn check_health(port: u16, timeout: Duration) -> Result<(), OpenCodeError> {
    let url = format!("http://127.0.0.1:{}/global/health", port);
    
    let client = ureq::AgentBuilder::new()
        .timeout(timeout)
        .build();
    
    match client.get(&url).call() {
        Ok(response) => {
            if response.status() == 200 {
                Ok(())
            } else {
                Err(OpenCodeError::HttpError(format!(
                    "health check returned status {}",
                    response.status()
                )))
            }
        }
        Err(ureq::Error::Status(code, _)) => {
            Err(OpenCodeError::HttpError(format!("HTTP status {}", code)))
        }
        Err(ureq::Error::Transport(_)) => {
            Err(OpenCodeError::ServerNotRunning(port))
        }
    }
}

/// List all OpenCode sessions.
pub fn list_sessions(port: u16, directory: Option<&str>) -> Result<Vec<OpenCodeSession>, OpenCodeError> {
    let url = format!("http://127.0.0.1:{}/session", port);
    
    let client = ureq::AgentBuilder::new()
        .timeout(Duration::from_secs(5))
        .build();
    
    let mut request = client.get(&url);
    
    if let Some(dir) = directory {
        request = request.set("X-OpenCode-Directory", dir);
    }
    
    match request.call() {
        Ok(response) => {
            response
                .into_json()
                .map_err(|e| OpenCodeError::HttpError(format!("failed to parse JSON: {}", e)))
        }
        Err(ureq::Error::Status(code, _)) => {
            Err(OpenCodeError::HttpError(format!("HTTP status {}", code)))
        }
        Err(ureq::Error::Transport(_)) => {
            Err(OpenCodeError::ServerNotRunning(port))
        }
    }
}

/// Check if a specific session exists.
pub fn session_exists(port: u16, session_id: &str, directory: Option<&str>) -> bool {
    list_sessions(port, directory)
        .map(|sessions| sessions.iter().any(|s| s.id == session_id))
        .unwrap_or(false)
}

/// Get the status of all sessions.
pub fn get_session_status(port: u16, directory: Option<&str>) -> Result<std::collections::HashMap<String, SessionStatus>, OpenCodeError> {
    let url = format!("http://127.0.0.1:{}/session/status", port);
    
    let client = ureq::AgentBuilder::new()
        .timeout(Duration::from_secs(5))
        .build();
    
    let mut request = client.get(&url);
    
    if let Some(dir) = directory {
        request = request.set("X-OpenCode-Directory", dir);
    }
    
    match request.call() {
        Ok(response) => {
            response
                .into_json()
                .map_err(|e| OpenCodeError::HttpError(format!("failed to parse JSON: {}", e)))
        }
        Err(ureq::Error::Status(code, _)) => {
            Err(OpenCodeError::HttpError(format!("HTTP status {}", code)))
        }
        Err(ureq::Error::Transport(_)) => {
            Err(OpenCodeError::ServerNotRunning(port))
        }
    }
}

/// Check if an OpenCode session is alive.
/// Returns true if the server is running and the session exists.
pub fn is_session_alive(port: u16, session_id: &str, directory: Option<&str>) -> bool {
    if !is_server_running(port) {
        return false;
    }
    
    session_exists(port, session_id, directory)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_session_status_serde() {
        let json = r#""idle""#;
        let status: SessionStatus = serde_json::from_str(json).unwrap();
        assert_eq!(status, SessionStatus::Idle);

        let json = r#""busy""#;
        let status: SessionStatus = serde_json::from_str(json).unwrap();
        assert_eq!(status, SessionStatus::Busy);
    }
}
