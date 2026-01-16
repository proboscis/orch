//! orch-core: Rust core library for orch - orchestrator for LLM CLIs.
//!
//! This library provides:
//! - Data models for issues, runs, and events
//! - File store implementation for vault operations
//! - Orchestrator for managing run lifecycles
//! - PyO3 bindings for Python integration (optional, feature = "python")
//! - CLI functionality
//! - Git and tmux operations
//! - Daemon for background monitoring

pub mod models;
pub mod store;
pub mod orchestrator;
#[cfg(feature = "python")]
pub mod python;
pub mod git;
pub mod tmux;
pub mod daemon;
pub mod agent;
pub mod cli;

// Re-export commonly used types
pub use models::{Event, EventType, Issue, IssueStatus, Phase, Run, RunRef, Status};
pub use store::{FileStore, ListRunsFilter, Store, StoreError};
pub use orchestrator::Orchestrator;
