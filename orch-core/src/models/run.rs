//! Run model representing a single execution of an issue.

use chrono::{DateTime, Local, Utc};
#[cfg(feature = "python")]
use pyo3::prelude::*;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::path::PathBuf;

use super::event::{Event, EventType};
use super::status::{Phase, Status};

/// Reference to a run (ISSUE_ID#RUN_ID).
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[cfg_attr(feature = "python", pyclass)]
pub struct RunRef {
    pub issue_id: String,
    pub run_id: String,
}

impl RunRef {
    /// Parse a RUN_REF string (ISSUE_ID#RUN_ID or just ISSUE_ID for latest).
    pub fn parse(ref_str: &str) -> Result<Self, String> {
        let ref_str = ref_str.trim();
        if ref_str.is_empty() {
            return Err("empty run reference".to_string());
        }

        let parts: Vec<&str> = ref_str.splitn(2, '#').collect();
        Ok(Self {
            issue_id: parts[0].to_string(),
            run_id: parts.get(1).map(|s| s.to_string()).unwrap_or_default(),
        })
    }

    /// Check if this ref points to the latest run (no RunID specified).
    pub fn is_latest(&self) -> bool {
        self.run_id.is_empty()
    }
}

impl std::fmt::Display for RunRef {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if self.run_id.is_empty() {
            write!(f, "{}", self.issue_id)
        } else {
            write!(f, "{}#{}", self.issue_id, self.run_id)
        }
    }
}

#[cfg(feature = "python")]
#[pymethods]
impl RunRef {
    #[new]
    #[pyo3(signature = (issue_id, run_id=None))]
    fn py_new(issue_id: String, run_id: Option<String>) -> Self {
        Self {
            issue_id,
            run_id: run_id.unwrap_or_default(),
        }
    }

    #[getter]
    fn issue_id(&self) -> String {
        self.issue_id.clone()
    }

    #[setter]
    fn set_issue_id(&mut self, issue_id: String) {
        self.issue_id = issue_id;
    }

    #[getter]
    fn run_id(&self) -> String {
        self.run_id.clone()
    }

    #[setter]
    fn set_run_id(&mut self, run_id: String) {
        self.run_id = run_id;
    }

    #[staticmethod]
    #[pyo3(name = "parse")]
    fn py_parse(ref_str: &str) -> PyResult<Self> {
        Self::parse(ref_str).map_err(|e| pyo3::exceptions::PyValueError::new_err(e))
    }

    fn __str__(&self) -> String {
        self.to_string()
    }

    fn __repr__(&self) -> String {
        format!("RunRef(issue_id='{}', run_id='{}')", self.issue_id, self.run_id)
    }
}

/// A single execution of an issue.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[cfg_attr(feature = "python", pyclass)]
pub struct Run {
    pub issue_id: String,
    pub run_id: String,
    pub path: PathBuf,

    // Derived from events
    pub status: Status,
    pub phase: Option<Phase>,
    pub events: Vec<Event>,
    pub started_at: Option<DateTime<Utc>>,
    pub updated_at: Option<DateTime<Utc>>,

    // Artifacts (from events)
    pub agent: String,
    pub model: String,
    pub model_variant: String,
    pub branch: String,
    pub worktree_path: String,
    pub tmux_session: String,
    pub tmux_window_id: String,
    pub pr_url: String,
    pub server_port: i32,
    pub opencode_session_id: String,

    // Frontmatter metadata
    pub continued_from: String,
}

impl Default for Run {
    fn default() -> Self {
        Self {
            issue_id: String::new(),
            run_id: String::new(),
            path: PathBuf::new(),
            status: Status::Queued,
            phase: None,
            events: Vec::new(),
            started_at: None,
            updated_at: None,
            agent: String::new(),
            model: String::new(),
            model_variant: String::new(),
            branch: String::new(),
            worktree_path: String::new(),
            tmux_session: String::new(),
            tmux_window_id: String::new(),
            pr_url: String::new(),
            server_port: 0,
            opencode_session_id: String::new(),
            continued_from: String::new(),
        }
    }
}

impl Run {
    /// Create a new run with the given issue and run IDs.
    pub fn new(issue_id: impl Into<String>, run_id: impl Into<String>) -> Self {
        Self {
            issue_id: issue_id.into(),
            run_id: run_id.into(),
            ..Default::default()
        }
    }

    /// Get the RunRef for this run.
    pub fn run_ref(&self) -> RunRef {
        RunRef {
            issue_id: self.issue_id.clone(),
            run_id: self.run_id.clone(),
        }
    }

    /// Return a 6-character hex identifier for the run (git-style).
    pub fn short_id(&self) -> String {
        generate_short_id(&self.issue_id, &self.run_id)
    }

    /// Derive status from events (last status event wins).
    pub fn get_status(&self) -> Status {
        for event in self.events.iter().rev() {
            if event.event_type == EventType::Status {
                if let Ok(status) = event.name.parse() {
                    return status;
                }
            }
        }
        Status::Queued
    }

    /// Derive phase from events (last phase event wins).
    pub fn get_phase(&self) -> Option<Phase> {
        for event in self.events.iter().rev() {
            if event.event_type == EventType::Phase {
                if let Ok(phase) = event.name.parse() {
                    return Some(phase);
                }
            }
        }
        None
    }

    /// Update status and artifacts from events.
    pub fn derive_state(&mut self) {
        self.status = self.get_status();
        self.phase = self.get_phase();

        // Extract artifacts from events
        for event in &self.events {
            if event.event_type == EventType::Artifact {
                match event.name.as_str() {
                    "worktree" => {
                        if let Some(path) = event.attrs.get("path") {
                            self.worktree_path = path.clone();
                        }
                    }
                    "branch" => {
                        if let Some(name) = event.attrs.get("name") {
                            self.branch = name.clone();
                        }
                    }
                    "session" => {
                        if let Some(name) = event.attrs.get("name") {
                            self.tmux_session = name.clone();
                        }
                    }
                    "window" => {
                        if let Some(id) = event.attrs.get("id") {
                            self.tmux_window_id = id.clone();
                        }
                    }
                    "pr" => {
                        if let Some(url) = event.attrs.get("url") {
                            self.pr_url = url.clone();
                        }
                    }
                    "server" => {
                        if let Some(port_str) = event.attrs.get("port") {
                            if let Ok(port) = port_str.parse() {
                                self.server_port = port;
                            }
                        }
                    }
                    "opencode_session" => {
                        if let Some(id) = event.attrs.get("id") {
                            self.opencode_session_id = id.clone();
                        }
                    }
                    "agent_model" => {
                        if self.model.is_empty() {
                            if let Some(model) = event.attrs.get("model") {
                                self.model = model.clone();
                            }
                        }
                        if self.model_variant.is_empty() {
                            if let Some(variant) = event.attrs.get("variant") {
                                self.model_variant = variant.clone();
                            }
                        }
                    }
                    _ => {}
                }
            }
        }

        // Derive timestamps
        if !self.events.is_empty() {
            self.started_at = Some(self.events[0].timestamp);
            self.updated_at = Some(self.events[self.events.len() - 1].timestamp);
        }
    }

    /// Return elapsed time since start as a human-readable string.
    pub fn elapsed_time(&self) -> String {
        let Some(started) = self.started_at else {
            return "-".to_string();
        };

        let end = self.updated_at.unwrap_or_else(Utc::now);
        let delta = end - started;
        let total_seconds = delta.num_seconds();

        let hours = total_seconds / 3600;
        let minutes = (total_seconds % 3600) / 60;
        let seconds = total_seconds % 60;

        if hours > 0 {
            format!("{}h{}m", hours, minutes)
        } else if minutes > 0 {
            format!("{}m{}s", minutes, seconds)
        } else {
            format!("{}s", seconds)
        }
    }

    /// Get the started_at timestamp as a string.
    pub fn started_at_str(&self) -> Option<String> {
        self.started_at.map(|dt| dt.format("%Y-%m-%dT%H:%M:%S%.3fZ").to_string())
    }

    /// Get the updated_at timestamp as a string.
    pub fn updated_at_str(&self) -> Option<String> {
        self.updated_at.map(|dt| dt.format("%Y-%m-%dT%H:%M:%S%.3fZ").to_string())
    }
}

/// Generate a 6-char hex ID from issue and run IDs.
pub fn generate_short_id(issue_id: &str, run_id: &str) -> String {
    let input = format!("{}#{}", issue_id, run_id);
    let mut hasher = Sha256::new();
    hasher.update(input.as_bytes());
    let result = hasher.finalize();
    hex::encode(&result[..3]) // 3 bytes = 6 hex chars
}

/// Generate a run ID using the convention YYYYMMDD-HHMMSS.
pub fn generate_run_id() -> String {
    Local::now().format("%Y%m%d-%H%M%S").to_string()
}

/// Generate a branch name using the convention.
pub fn generate_branch_name(issue_id: &str, run_id: &str) -> String {
    format!("issue/{}/run-{}", issue_id, run_id)
}

/// Generate a tmux session name using the convention.
pub fn generate_tmux_session(issue_id: &str, run_id: &str) -> String {
    format!("run-{}-{}", issue_id, run_id)
}

/// Generate a worktree directory name using a short ID.
pub fn generate_worktree_name(issue_id: &str, run_id: &str, agent: &str) -> String {
    let agent = if agent.trim().is_empty() { "unknown" } else { agent.trim() };
    format!("{}_{}_{}",
        generate_short_id(issue_id, run_id),
        agent,
        run_id
    )
}

#[cfg(feature = "python")]
#[pymethods]
impl Run {
    #[new]
    fn py_new(issue_id: String, run_id: String) -> Self {
        Self::new(issue_id, run_id)
    }

    #[getter]
    fn issue_id(&self) -> String {
        self.issue_id.clone()
    }

    #[setter]
    fn set_issue_id(&mut self, issue_id: String) {
        self.issue_id = issue_id;
    }

    #[getter]
    fn run_id(&self) -> String {
        self.run_id.clone()
    }

    #[setter]
    fn set_run_id(&mut self, run_id: String) {
        self.run_id = run_id;
    }

    #[getter]
    fn path(&self) -> String {
        self.path.to_string_lossy().to_string()
    }

    #[setter]
    fn set_path(&mut self, path: String) {
        self.path = PathBuf::from(path);
    }

    #[getter]
    fn status(&self) -> Status {
        self.status
    }

    #[setter]
    fn set_status(&mut self, status: Status) {
        self.status = status;
    }

    #[getter]
    fn phase(&self) -> Option<Phase> {
        self.phase
    }

    #[setter]
    fn set_phase(&mut self, phase: Option<Phase>) {
        self.phase = phase;
    }

    #[getter]
    fn events(&self) -> Vec<Event> {
        self.events.clone()
    }

    #[getter]
    fn started_at(&self) -> Option<String> {
        self.started_at_str()
    }

    #[getter]
    fn updated_at(&self) -> Option<String> {
        self.updated_at_str()
    }

    #[getter]
    fn agent(&self) -> String {
        self.agent.clone()
    }

    #[setter]
    fn set_agent(&mut self, agent: String) {
        self.agent = agent;
    }

    #[getter]
    fn model(&self) -> String {
        self.model.clone()
    }

    #[setter]
    fn set_model(&mut self, model: String) {
        self.model = model;
    }

    #[getter]
    fn model_variant(&self) -> String {
        self.model_variant.clone()
    }

    #[setter]
    fn set_model_variant(&mut self, model_variant: String) {
        self.model_variant = model_variant;
    }

    #[getter]
    fn branch(&self) -> String {
        self.branch.clone()
    }

    #[setter]
    fn set_branch(&mut self, branch: String) {
        self.branch = branch;
    }

    #[getter]
    fn worktree_path(&self) -> String {
        self.worktree_path.clone()
    }

    #[setter]
    fn set_worktree_path(&mut self, worktree_path: String) {
        self.worktree_path = worktree_path;
    }

    #[getter]
    fn tmux_session(&self) -> String {
        self.tmux_session.clone()
    }

    #[setter]
    fn set_tmux_session(&mut self, tmux_session: String) {
        self.tmux_session = tmux_session;
    }

    #[getter]
    fn tmux_window_id(&self) -> String {
        self.tmux_window_id.clone()
    }

    #[setter]
    fn set_tmux_window_id(&mut self, tmux_window_id: String) {
        self.tmux_window_id = tmux_window_id;
    }

    #[getter]
    fn pr_url(&self) -> String {
        self.pr_url.clone()
    }

    #[setter]
    fn set_pr_url(&mut self, pr_url: String) {
        self.pr_url = pr_url;
    }

    #[getter]
    fn server_port(&self) -> i32 {
        self.server_port
    }

    #[setter]
    fn set_server_port(&mut self, server_port: i32) {
        self.server_port = server_port;
    }

    #[getter]
    fn opencode_session_id(&self) -> String {
        self.opencode_session_id.clone()
    }

    #[setter]
    fn set_opencode_session_id(&mut self, opencode_session_id: String) {
        self.opencode_session_id = opencode_session_id;
    }

    #[getter]
    fn continued_from(&self) -> String {
        self.continued_from.clone()
    }

    #[setter]
    fn set_continued_from(&mut self, continued_from: String) {
        self.continued_from = continued_from;
    }

    #[pyo3(name = "ref")]
    fn py_ref(&self) -> String {
        format!("{}#{}", self.issue_id, self.run_id)
    }

    #[pyo3(name = "short_id")]
    fn py_short_id(&self) -> String {
        self.short_id()
    }

    #[pyo3(name = "derive_state")]
    fn py_derive_state(&mut self) {
        self.derive_state()
    }

    #[pyo3(name = "elapsed_time")]
    fn py_elapsed_time(&self) -> String {
        self.elapsed_time()
    }

    fn __str__(&self) -> String {
        format!("Run({}#{})", self.issue_id, self.run_id)
    }

    fn __repr__(&self) -> String {
        format!(
            "Run(issue_id='{}', run_id='{}', status={:?})",
            self.issue_id, self.run_id, self.status
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_run_ref_parse() {
        let ref1 = RunRef::parse("my-issue#20240115-103000").unwrap();
        assert_eq!(ref1.issue_id, "my-issue");
        assert_eq!(ref1.run_id, "20240115-103000");
        assert!(!ref1.is_latest());

        let ref2 = RunRef::parse("my-issue").unwrap();
        assert_eq!(ref2.issue_id, "my-issue");
        assert_eq!(ref2.run_id, "");
        assert!(ref2.is_latest());
    }

    #[test]
    fn test_short_id() {
        let id1 = generate_short_id("my-issue", "20240115-103000");
        let id2 = generate_short_id("my-issue", "20240115-103000");
        assert_eq!(id1, id2);
        assert_eq!(id1.len(), 6);

        let id3 = generate_short_id("other-issue", "20240115-103000");
        assert_ne!(id1, id3);
    }

    #[test]
    fn test_generate_run_id() {
        let id = generate_run_id();
        assert!(id.len() == 15); // YYYYMMDD-HHMMSS
        assert!(id.contains('-'));
    }

    #[test]
    fn test_generate_branch_name() {
        let branch = generate_branch_name("my-issue", "20240115-103000");
        assert_eq!(branch, "issue/my-issue/run-20240115-103000");
    }
}
