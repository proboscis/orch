//! Store implementations for reading and writing vault data.

mod file;

pub use file::FileStore;

use crate::models::{Event, Issue, IssueStatus, Run, RunRef, Status};
use std::path::PathBuf;
use thiserror::Error;

/// Errors that can occur during store operations.
#[derive(Error, Debug)]
pub enum StoreError {
    #[error("issue not found: {0}")]
    IssueNotFound(String),

    #[error("run not found: {0}")]
    RunNotFound(String),

    #[error("vault path does not exist: {0}")]
    VaultNotFound(PathBuf),

    #[error("vault path is not a directory: {0}")]
    VaultNotDirectory(PathBuf),

    #[error("run already exists: {0}#{1}")]
    RunAlreadyExists(String, String),

    #[error("ambiguous run ID '{0}': matches {1} runs")]
    AmbiguousRunId(String, usize),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    #[error("parse error: {0}")]
    Parse(String),
}

/// Filter criteria for listing runs.
#[derive(Debug, Default, Clone)]
pub struct ListRunsFilter {
    pub issue_id: Option<String>,
    pub status: Vec<Status>,
    pub limit: Option<usize>,
    pub since: Option<String>,
}

impl ListRunsFilter {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn with_issue_id(mut self, issue_id: impl Into<String>) -> Self {
        self.issue_id = Some(issue_id.into());
        self
    }

    pub fn with_status(mut self, status: Vec<Status>) -> Self {
        self.status = status;
        self
    }

    pub fn with_limit(mut self, limit: usize) -> Self {
        self.limit = Some(limit);
        self
    }

    pub fn with_since(mut self, since: impl Into<String>) -> Self {
        self.since = Some(since.into());
        self
    }
}

/// Store trait for vault backends.
pub trait Store: Send + Sync {
    /// Resolve an issue by ID.
    fn resolve_issue(&self, issue_id: &str) -> Result<Issue, StoreError>;

    /// List all issues in the vault.
    fn list_issues(&self) -> Result<Vec<Issue>, StoreError>;

    /// Set the status of an issue.
    fn set_issue_status(&self, issue_id: &str, status: IssueStatus) -> Result<(), StoreError>;

    /// Create a new run for an issue.
    fn create_run(
        &self,
        issue_id: &str,
        run_id: &str,
        metadata: std::collections::HashMap<String, String>,
    ) -> Result<Run, StoreError>;

    /// Append an event to a run.
    fn append_event(&self, run_ref: &RunRef, event: &Event) -> Result<(), StoreError>;

    /// List runs matching the filter.
    fn list_runs(&self, filter: &ListRunsFilter) -> Result<Vec<Run>, StoreError>;

    /// Get a run by reference.
    fn get_run(&self, run_ref: &RunRef) -> Result<Run, StoreError>;

    /// Find a run by its short ID prefix (2-6 hex chars).
    fn get_run_by_short_id(&self, short_id: &str) -> Result<Run, StoreError>;

    /// Get the latest run for an issue.
    fn get_latest_run(&self, issue_id: &str) -> Result<Run, StoreError>;

    /// Get the vault root path.
    fn vault_path(&self) -> &std::path::Path;
}
