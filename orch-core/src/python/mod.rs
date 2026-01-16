//! Python bindings for orch-core using PyO3.
//!
//! This module exports the core orch functionality to Python,
//! allowing the Python Textual TUI to use the Rust backend.

use pyo3::prelude::*;
use std::collections::HashMap;

use crate::models::{Event, EventType, Issue, IssueStatus, Phase, Run, RunRef, Status};
use crate::store::{FileStore, ListRunsFilter, Store};

/// Python wrapper for the FileStore.
#[pyclass(name = "VaultStore")]
pub struct PyVaultStore {
    store: FileStore,
}

#[pymethods]
impl PyVaultStore {
    #[new]
    fn new(vault_path: String) -> PyResult<Self> {
        let store = FileStore::new(&vault_path)
            .map_err(|e| pyo3::exceptions::PyIOError::new_err(e.to_string()))?;
        Ok(Self { store })
    }

    /// Get the vault root path.
    #[getter]
    fn vault_path(&self) -> String {
        self.store.vault_path().to_string_lossy().to_string()
    }

    /// Resolve an issue by ID.
    fn resolve_issue(&self, issue_id: &str) -> PyResult<Issue> {
        self.store
            .resolve_issue(issue_id)
            .map_err(|e| pyo3::exceptions::PyValueError::new_err(e.to_string()))
    }

    /// List all issues in the vault.
    fn list_issues(&self) -> PyResult<Vec<Issue>> {
        self.store
            .list_issues()
            .map_err(|e| pyo3::exceptions::PyIOError::new_err(e.to_string()))
    }

    /// Set the status of an issue.
    fn set_issue_status(&self, issue_id: &str, status: IssueStatus) -> PyResult<()> {
        self.store
            .set_issue_status(issue_id, status)
            .map_err(|e| pyo3::exceptions::PyIOError::new_err(e.to_string()))
    }

    /// List runs with optional filtering.
    #[pyo3(signature = (issue_id=None, status=None, limit=None, since=None))]
    fn list_runs(
        &self,
        issue_id: Option<String>,
        status: Option<Vec<Status>>,
        limit: Option<usize>,
        since: Option<String>,
    ) -> PyResult<Vec<Run>> {
        let mut filter = ListRunsFilter::default();
        if let Some(id) = issue_id {
            filter.issue_id = Some(id);
        }
        if let Some(s) = status {
            filter.status = s;
        }
        if let Some(l) = limit {
            filter.limit = Some(l);
        }
        if let Some(s) = since {
            filter.since = Some(s);
        }

        self.store
            .list_runs(&filter)
            .map_err(|e| pyo3::exceptions::PyIOError::new_err(e.to_string()))
    }

    /// Get a run by reference (issue_id#run_id or just issue_id for latest).
    fn get_run(&self, ref_str: &str) -> PyResult<Run> {
        let run_ref = RunRef::parse(ref_str)
            .map_err(|e| pyo3::exceptions::PyValueError::new_err(e))?;
        self.store
            .get_run(&run_ref)
            .map_err(|e| pyo3::exceptions::PyValueError::new_err(e.to_string()))
    }

    /// Get a run by its short ID prefix (2-6 hex chars).
    fn get_run_by_short_id(&self, short_id: &str) -> PyResult<Run> {
        self.store
            .get_run_by_short_id(short_id)
            .map_err(|e| pyo3::exceptions::PyValueError::new_err(e.to_string()))
    }

    /// Get the latest run for an issue.
    fn get_latest_run(&self, issue_id: &str) -> PyResult<Run> {
        self.store
            .get_latest_run(issue_id)
            .map_err(|e| pyo3::exceptions::PyValueError::new_err(e.to_string()))
    }

    /// Create a new run for an issue.
    #[pyo3(signature = (issue_id, run_id, metadata=None))]
    fn create_run(
        &self,
        issue_id: &str,
        run_id: &str,
        metadata: Option<HashMap<String, String>>,
    ) -> PyResult<Run> {
        self.store
            .create_run(issue_id, run_id, metadata.unwrap_or_default())
            .map_err(|e| pyo3::exceptions::PyIOError::new_err(e.to_string()))
    }

    /// Append an event to a run.
    fn append_event(&self, ref_str: &str, event: &Event) -> PyResult<()> {
        let run_ref = RunRef::parse(ref_str)
            .map_err(|e| pyo3::exceptions::PyValueError::new_err(e))?;
        self.store
            .append_event(&run_ref, event)
            .map_err(|e| pyo3::exceptions::PyIOError::new_err(e.to_string()))
    }
}

/// Generate a run ID using the convention YYYYMMDD-HHMMSS.
#[pyfunction]
fn generate_run_id() -> String {
    crate::models::run::generate_run_id()
}

/// Generate a branch name using the convention.
#[pyfunction]
fn generate_branch_name(issue_id: &str, run_id: &str) -> String {
    crate::models::run::generate_branch_name(issue_id, run_id)
}

/// Generate a tmux session name using the convention.
#[pyfunction]
fn generate_tmux_session(issue_id: &str, run_id: &str) -> String {
    crate::models::run::generate_tmux_session(issue_id, run_id)
}

/// Generate a worktree directory name using a short ID.
#[pyfunction]
fn generate_worktree_name(issue_id: &str, run_id: &str, agent: &str) -> String {
    crate::models::run::generate_worktree_name(issue_id, run_id, agent)
}

/// Generate a 6-char hex ID from issue and run IDs.
#[pyfunction]
fn generate_short_id(issue_id: &str, run_id: &str) -> String {
    crate::models::run::generate_short_id(issue_id, run_id)
}

/// Python module initialization.
#[pymodule]
pub fn orch_core(m: &Bound<'_, PyModule>) -> PyResult<()> {
    // Classes
    m.add_class::<PyVaultStore>()?;
    m.add_class::<Issue>()?;
    m.add_class::<IssueStatus>()?;
    m.add_class::<Run>()?;
    m.add_class::<RunRef>()?;
    m.add_class::<Event>()?;
    m.add_class::<EventType>()?;
    m.add_class::<Status>()?;
    m.add_class::<Phase>()?;

    // Functions
    m.add_function(wrap_pyfunction!(generate_run_id, m)?)?;
    m.add_function(wrap_pyfunction!(generate_branch_name, m)?)?;
    m.add_function(wrap_pyfunction!(generate_tmux_session, m)?)?;
    m.add_function(wrap_pyfunction!(generate_worktree_name, m)?)?;
    m.add_function(wrap_pyfunction!(generate_short_id, m)?)?;

    Ok(())
}
