use std::collections::HashMap;
use std::time::Duration;

use serde::{Deserialize, Serialize};

use crate::models::Run;
use super::alive::AliveStatus;

const DEFAULT_TIMEOUT: Duration = Duration::from_secs(5);

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OpenCodeSession {
    pub id: String,
    pub title: String,
    #[serde(default)]
    pub directory: String,
    #[serde(default, rename = "parentID")]
    pub parent_id: String,
    #[serde(default)]
    pub time: SessionTime,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct SessionTime {
    pub created: i64,
    pub updated: i64,
}

impl OpenCodeSession {
    pub fn updated_at(&self) -> chrono::DateTime<chrono::Utc> {
        chrono::DateTime::from_timestamp_millis(self.time.updated)
            .unwrap_or_else(chrono::Utc::now)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HealthResponse {
    pub healthy: bool,
    #[serde(default)]
    pub version: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SessionStatus {
    Idle,
    Busy,
    Retry,
}

impl std::fmt::Display for SessionStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            SessionStatus::Idle => write!(f, "idle"),
            SessionStatus::Busy => write!(f, "busy"),
            SessionStatus::Retry => write!(f, "retry"),
        }
    }
}

pub struct OpenCodeClient {
    base_url: String,
    port: i32,
    timeout: Duration,
}

impl OpenCodeClient {
    pub fn new(port: i32) -> Self {
        Self {
            base_url: format!("http://127.0.0.1:{}", port),
            port,
            timeout: DEFAULT_TIMEOUT,
        }
    }

    pub fn with_timeout(mut self, timeout: Duration) -> Self {
        self.timeout = timeout;
        self
    }

    pub fn port(&self) -> i32 {
        self.port
    }

    pub fn is_server_running(&self) -> bool {
        self.health().map(|h| h.healthy).unwrap_or(false)
    }

    pub fn health(&self) -> Result<HealthResponse, OpenCodeError> {
        let url = format!("{}/global/health", self.base_url);
        
        let response = ureq::get(&url)
            .timeout(self.timeout)
            .call()
            .map_err(|e| OpenCodeError::Request(e.to_string()))?;

        let health: HealthResponse = response.into_json()
            .map_err(|e| OpenCodeError::Parse(e.to_string()))?;

        Ok(health)
    }

    pub fn get_sessions(&self, directory: Option<&str>) -> Result<Vec<OpenCodeSession>, OpenCodeError> {
        let url = format!("{}/session", self.base_url);
        
        let mut request = ureq::get(&url).timeout(self.timeout);
        
        if let Some(dir) = directory {
            request = request.set("X-OpenCode-Directory", dir);
        }

        let response = request.call()
            .map_err(|e| OpenCodeError::Request(e.to_string()))?;

        let sessions: Vec<OpenCodeSession> = response.into_json()
            .map_err(|e| OpenCodeError::Parse(e.to_string()))?;

        Ok(sessions)
    }

    pub fn get_session(&self, session_id: &str, directory: Option<&str>) -> Result<OpenCodeSession, OpenCodeError> {
        let url = format!("{}/session/{}", self.base_url, session_id);
        
        let mut request = ureq::get(&url).timeout(self.timeout);
        
        if let Some(dir) = directory {
            request = request.set("X-OpenCode-Directory", dir);
        }

        let response = request.call()
            .map_err(|e| {
                if e.to_string().contains("404") {
                    OpenCodeError::SessionNotFound(session_id.to_string())
                } else {
                    OpenCodeError::Request(e.to_string())
                }
            })?;

        let session: OpenCodeSession = response.into_json()
            .map_err(|e| OpenCodeError::Parse(e.to_string()))?;

        Ok(session)
    }

    pub fn get_session_status(&self, directory: Option<&str>) -> Result<HashMap<String, SessionStatus>, OpenCodeError> {
        let url = format!("{}/session/status", self.base_url);
        
        let mut request = ureq::get(&url).timeout(self.timeout);
        
        if let Some(dir) = directory {
            request = request.set("X-OpenCode-Directory", dir);
        }

        let response = request.call()
            .map_err(|e| OpenCodeError::Request(e.to_string()))?;

        let body = response.into_string()
            .map_err(|e| OpenCodeError::Parse(e.to_string()))?;

        parse_session_status_response(&body)
    }

    pub fn get_single_session_status(&self, session_id: &str, directory: Option<&str>) -> Result<(SessionStatus, bool), OpenCodeError> {
        let status_map = self.get_session_status(directory)?;
        
        if let Some(&status) = status_map.get(session_id) {
            Ok((status, true))
        } else {
            Ok((SessionStatus::Idle, false))
        }
    }
}

fn parse_session_status_response(body: &str) -> Result<HashMap<String, SessionStatus>, OpenCodeError> {
    if let Ok(string_map) = serde_json::from_str::<HashMap<String, String>>(body) {
        let result: HashMap<String, SessionStatus> = string_map
            .into_iter()
            .filter_map(|(k, v)| {
                let status = match v.as_str() {
                    "idle" => SessionStatus::Idle,
                    "busy" => SessionStatus::Busy,
                    "retry" => SessionStatus::Retry,
                    _ => return None,
                };
                Some((k, status))
            })
            .collect();
        return Ok(result);
    }

    #[derive(Deserialize)]
    struct StatusObject {
        #[serde(rename = "type")]
        status_type: String,
    }

    if let Ok(object_map) = serde_json::from_str::<HashMap<String, StatusObject>>(body) {
        let result: HashMap<String, SessionStatus> = object_map
            .into_iter()
            .filter_map(|(k, v)| {
                let status = match v.status_type.as_str() {
                    "idle" => SessionStatus::Idle,
                    "busy" => SessionStatus::Busy,
                    "retry" => SessionStatus::Retry,
                    _ => return None,
                };
                Some((k, status))
            })
            .collect();
        return Ok(result);
    }

    Err(OpenCodeError::Parse(format!("unable to parse session status response: {}", body)))
}

#[derive(Debug, thiserror::Error)]
pub enum OpenCodeError {
    #[error("request failed: {0}")]
    Request(String),
    
    #[error("failed to parse response: {0}")]
    Parse(String),
    
    #[error("session not found: {0}")]
    SessionNotFound(String),
    
    #[error("server not running on port {0}")]
    ServerNotRunning(i32),
}

pub fn check_opencode_health(port: i32, session_id: &str, directory: Option<&str>) -> Result<AliveStatus, OpenCodeError> {
    let client = OpenCodeClient::new(port);

    if !client.is_server_running() {
        return Ok(AliveStatus::Dead);
    }

    let status_map = client.get_session_status(directory)?;

    if let Some(&status) = status_map.get(session_id) {
        return match status {
            SessionStatus::Busy => Ok(AliveStatus::Alive),
            SessionStatus::Idle => Ok(AliveStatus::Alive),
            SessionStatus::Retry => Ok(AliveStatus::Alive),
        };
    }

    if has_active_busy_children(&client, session_id, directory)? {
        return Ok(AliveStatus::Alive);
    }

    if has_recent_activity(&client, session_id, directory)? {
        return Ok(AliveStatus::Alive);
    }

    if status_map.contains_key(session_id) {
        return Ok(AliveStatus::Alive);
    }

    Ok(AliveStatus::Dead)
}

fn has_active_busy_children(client: &OpenCodeClient, parent_session_id: &str, directory: Option<&str>) -> Result<bool, OpenCodeError> {
    let status_map = client.get_session_status(directory)?;

    for (session_id, status) in &status_map {
        if *status != SessionStatus::Busy {
            continue;
        }
        
        if let Ok(session) = client.get_session(session_id, directory) {
            if session.parent_id == parent_session_id {
                return Ok(true);
            }
        }
    }

    Ok(false)
}

fn has_recent_activity(client: &OpenCodeClient, session_id: &str, directory: Option<&str>) -> Result<bool, OpenCodeError> {
    const RECENT_ACTIVITY_THRESHOLD: chrono::Duration = chrono::Duration::seconds(30);

    let session = client.get_session(session_id, directory)?;
    let time_since_update = chrono::Utc::now() - session.updated_at();

    Ok(time_since_update < RECENT_ACTIVITY_THRESHOLD)
}

pub fn list_opencode_sessions(port: i32) -> Result<Vec<OpenCodeSession>, OpenCodeError> {
    let client = OpenCodeClient::new(port);
    
    if !client.is_server_running() {
        return Err(OpenCodeError::ServerNotRunning(port));
    }
    
    client.get_sessions(None)
}

pub fn check_opencode_batch(runs: &[&Run]) -> HashMap<String, AliveStatus> {
    let mut results = HashMap::new();
    let mut ports_checked: HashMap<i32, (bool, Option<HashMap<String, SessionStatus>>)> = HashMap::new();

    for run in runs {
        let ref_key = format!("{}#{}", run.issue_id, run.run_id);

        if run.server_port <= 0 {
            results.insert(ref_key, AliveStatus::Unknown);
            continue;
        }

        let port = run.server_port;

        let (server_running, status_map) = ports_checked
            .entry(port)
            .or_insert_with(|| {
                let client = OpenCodeClient::new(port);
                let running = client.is_server_running();
                let status = if running {
                    let directory = if run.worktree_path.is_empty() { 
                        None 
                    } else { 
                        Some(run.worktree_path.as_str()) 
                    };
                    client.get_session_status(directory).ok()
                } else {
                    None
                };
                (running, status)
            });

        if !*server_running {
            results.insert(ref_key, AliveStatus::Dead);
            continue;
        }

        if run.opencode_session_id.is_empty() {
            results.insert(ref_key, AliveStatus::Unknown);
            continue;
        }

        let status = if let Some(ref map) = status_map {
            if map.contains_key(&run.opencode_session_id) {
                AliveStatus::Alive
            } else {
                AliveStatus::Dead
            }
        } else {
            AliveStatus::Unknown
        };

        results.insert(ref_key, status);
    }

    results
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_session_status_string_format() {
        let body = r#"{"ses_abc123": "busy", "ses_def456": "idle"}"#;
        let result = parse_session_status_response(body).unwrap();
        
        assert_eq!(result.get("ses_abc123"), Some(&SessionStatus::Busy));
        assert_eq!(result.get("ses_def456"), Some(&SessionStatus::Idle));
    }

    #[test]
    fn test_parse_session_status_object_format() {
        let body = r#"{"ses_abc123": {"type": "busy"}, "ses_def456": {"type": "idle"}}"#;
        let result = parse_session_status_response(body).unwrap();
        
        assert_eq!(result.get("ses_abc123"), Some(&SessionStatus::Busy));
        assert_eq!(result.get("ses_def456"), Some(&SessionStatus::Idle));
    }

    #[test]
    fn test_session_status_display() {
        assert_eq!(format!("{}", SessionStatus::Idle), "idle");
        assert_eq!(format!("{}", SessionStatus::Busy), "busy");
        assert_eq!(format!("{}", SessionStatus::Retry), "retry");
    }

    #[test]
    fn test_opencode_client_new() {
        let client = OpenCodeClient::new(4096);
        assert_eq!(client.port(), 4096);
    }
}
