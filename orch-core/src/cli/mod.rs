//! CLI commands for orch.
//!
//! This module implements the orch CLI using clap.

mod ps;
mod issue;
mod show;

use clap::{Parser, Subcommand};
use std::path::PathBuf;

pub use ps::PsCommand;
pub use issue::IssueCommand;
pub use show::ShowCommand;

/// Exit codes as per spec
pub const EXIT_OK: i32 = 0;
pub const EXIT_ISSUE_NOT_FOUND: i32 = 2;
pub const EXIT_WORKTREE_ERROR: i32 = 3;
pub const EXIT_TMUX_ERROR: i32 = 4;
pub const EXIT_AGENT_ERROR: i32 = 5;
pub const EXIT_RUN_NOT_FOUND: i32 = 6;
pub const EXIT_QUESTION_NOT_FOUND: i32 = 7;
pub const EXIT_INTERNAL_ERROR: i32 = 10;

/// Orchestrator for multiple LLM CLIs.
#[derive(Parser, Debug)]
#[command(name = "orch")]
#[command(author, version, about, long_about = None)]
pub struct Cli {
    /// Path to vault (or set ORCH_VAULT)
    #[arg(long, env = "ORCH_VAULT", global = true)]
    pub vault: Option<PathBuf>,

    /// Backend type
    #[arg(long, default_value = "file", global = true)]
    pub backend: String,

    /// Output in JSON format
    #[arg(long, global = true)]
    pub json: bool,

    /// Output in TSV format (for fzf)
    #[arg(long, global = true)]
    pub tsv: bool,

    /// Suppress human-readable output
    #[arg(long, global = true)]
    pub quiet: bool,

    /// Log level
    #[arg(long, default_value = "warn", global = true)]
    pub log_level: String,

    #[command(subcommand)]
    pub command: Commands,
}

#[derive(Subcommand, Debug)]
pub enum Commands {
    /// List issues
    Issue(IssueCommand),

    /// List runs (ps)
    Ps(PsCommand),

    /// Show details of a run or issue
    Show(ShowCommand),

    /// Start a new run for an issue
    Run {
        /// Issue ID to run
        issue_id: String,

        /// Agent to use
        #[arg(long, short)]
        agent: Option<String>,

        /// Model to use
        #[arg(long, short)]
        model: Option<String>,

        /// Enable verbose output
        #[arg(long, short)]
        verbose: bool,
    },

    /// Continue from an existing run
    Continue {
        /// Issue ID or run reference
        issue_ref: String,

        /// Branch to continue from
        #[arg(long)]
        branch: Option<String>,
    },

    /// Attach to a run's tmux session
    Attach {
        /// Run reference (short ID, issue#run, or issue)
        run_ref: String,
    },

    /// Stop runs
    Stop {
        /// Run reference (or issue ID to stop all runs for issue)
        run_ref: Option<String>,

        /// Stop all runs
        #[arg(long)]
        all: bool,
    },

    /// Mark an issue as resolved
    Resolve {
        /// Issue ID
        issue_id: String,
    },

    /// Open an issue in the editor
    Open {
        /// Issue ID
        issue_id: String,
    },

    /// Launch the monitor TUI
    Monitor,

    /// Manage the daemon
    Daemon {
        /// Stop the daemon
        #[arg(long)]
        stop: bool,

        /// Restart the daemon
        #[arg(long)]
        restart: bool,
    },

    /// Repair orch state
    Repair,

    /// Delete a run
    Delete {
        /// Run reference
        run_ref: String,

        /// Force deletion without confirmation
        #[arg(long, short)]
        force: bool,
    },

    /// Execute a command in a run's worktree
    Exec {
        /// Run reference
        run_ref: String,

        /// Command to execute
        #[arg(trailing_var_arg = true)]
        command: Vec<String>,
    },

    /// Send input to a run's agent
    Send {
        /// Run reference
        run_ref: String,

        /// Input to send
        input: String,
    },

    /// Capture output from a run
    Capture {
        /// Run reference
        run_ref: String,

        /// Output file path
        #[arg(long, short)]
        output: Option<PathBuf>,
    },

    /// Capture output from all runs
    CaptureAll {
        /// Output directory path
        #[arg(long, short)]
        output: Option<PathBuf>,
    },

    /// List available models
    Models,
}

/// Get the vault path from CLI args or environment.
pub fn get_vault_path(cli: &Cli) -> Result<PathBuf, String> {
    if let Some(ref vault) = cli.vault {
        Ok(vault.clone())
    } else if let Ok(vault) = std::env::var("ORCH_VAULT") {
        Ok(PathBuf::from(vault))
    } else {
        // Try to find config file
        Err("vault path not specified (use --vault, set ORCH_VAULT, or create .orch/config.yaml)".to_string())
    }
}
