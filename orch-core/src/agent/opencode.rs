//! OpenCode HTTP health check functionality.

use serde::{Deserialize, Serialize};
use std::time::Duration;

use super::AgentError;

const DEFAULT_TIMEOUT: Duration = Duration::from_secs(5);

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OpenCodeSession {
    pub session_id: String,
    pub status: String,
    pub model: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct HealthResponse {
    healthy: bool,
    #[serde(default)]
    version: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct SessionInfo {
    #[serde(rename = "id")]
    session_id: String,
    #[serde(default)]
    title: String,
}

pub fn check_opencode_health(
    port: i32,
    session_id: &str,
) -> Result<OpenCodeSession, AgentError> {
    let url = format!("http://127.0.0.1:{}/session/{}", port, session_id);

    let client = reqwest::blocking::Client::builder()
        .timeout(DEFAULT_TIMEOUT)
        .build()
        .map_err(|e| AgentError::HealthCheckFailed(e.to_string()))?;

    let response = client
        .get(&url)
        .send()
        .map_err(|e| AgentError::HealthCheckFailed(e.to_string()))?;

    if !response.status().is_success() {
        return Err(AgentError::HealthCheckFailed(format!(
            "HTTP {}",
            response.status()
        )));
    }

    let session: SessionInfo = response
        .json()
        .map_err(|e| AgentError::HealthCheckFailed(e.to_string()))?;

    Ok(OpenCodeSession {
        session_id: session.session_id,
        status: "running".to_string(),
        model: None,
    })
}

pub fn list_opencode_sessions(port: i32) -> Result<Vec<OpenCodeSession>, AgentError> {
    let url = format!("http://127.0.0.1:{}/session", port);

    let client = reqwest::blocking::Client::builder()
        .timeout(DEFAULT_TIMEOUT)
        .build()
        .map_err(|e| AgentError::HealthCheckFailed(e.to_string()))?;

    let response = client
        .get(&url)
        .send()
        .map_err(|e| AgentError::HealthCheckFailed(e.to_string()))?;

    if !response.status().is_success() {
        return Err(AgentError::HealthCheckFailed(format!(
            "HTTP {}",
            response.status()
        )));
    }

    let sessions: Vec<SessionInfo> = response
        .json()
        .map_err(|e| AgentError::HealthCheckFailed(e.to_string()))?;

    Ok(sessions
        .into_iter()
        .map(|s| OpenCodeSession {
            session_id: s.session_id,
            status: "running".to_string(),
            model: None,
        })
        .collect())
}

pub fn is_opencode_server_alive(port: i32) -> bool {
    let url = format!("http://127.0.0.1:{}/global/health", port);

    let client = match reqwest::blocking::Client::builder()
        .timeout(DEFAULT_TIMEOUT)
        .build()
    {
        Ok(c) => c,
        Err(_) => return false,
    };

    match client.get(&url).send() {
        Ok(response) => {
            if !response.status().is_success() {
                return false;
            }

            match response.json::<HealthResponse>() {
                Ok(health) => health.healthy,
                Err(_) => false,
            }
        }
        Err(_) => false,
    }
}

pub fn is_opencode_session_alive(port: i32, session_id: &str) -> bool {
    if !is_opencode_server_alive(port) {
        return false;
    }

    check_opencode_health(port, session_id).is_ok()
}
