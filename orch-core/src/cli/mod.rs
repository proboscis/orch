//! CLI commands for orch.
//!
//! This module implements the orch CLI using clap.

mod ps;
mod issue;
mod show;

use anyhow::{Context, Result, bail};
use clap::{Parser, Subcommand};
use std::path::PathBuf;

pub use ps::PsCommand;
pub use issue::IssueCommand;
pub use show::ShowCommand;

use crate::store::{FileStore, Store, StoreError};
use crate::models::IssueStatus;
use crate::orchestrator::Orchestrator;

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
pub fn get_vault_path(cli: &Cli) -> Result<PathBuf> {
    if let Some(ref vault) = cli.vault {
        Ok(vault.clone())
    } else if let Ok(vault) = std::env::var("ORCH_VAULT") {
        Ok(PathBuf::from(vault))
    } else {
        bail!("vault path not specified (use --vault, set ORCH_VAULT, or create .orch/config.yaml)")
    }
}

/// Map StoreError to appropriate exit code.
pub fn exit_code_for_error(err: &anyhow::Error) -> i32 {
    if let Some(store_err) = err.downcast_ref::<StoreError>() {
        match store_err {
            StoreError::IssueNotFound(_) => EXIT_ISSUE_NOT_FOUND,
            StoreError::RunNotFound(_) => EXIT_RUN_NOT_FOUND,
            StoreError::AmbiguousRunId(_, _) => EXIT_RUN_NOT_FOUND,
            _ => EXIT_INTERNAL_ERROR,
        }
    } else {
        EXIT_INTERNAL_ERROR
    }
}

/// Execute the CLI command.
pub fn execute(cli: Cli) -> Result<()> {
    let vault_path = get_vault_path(&cli)?;

    match cli.command {
        Commands::Issue(cmd) => {
            cmd.execute(&vault_path, cli.json, cli.tsv)
        }
        Commands::Ps(cmd) => {
            cmd.execute(&vault_path, cli.json, cli.tsv)
        }
        Commands::Show(cmd) => {
            cmd.execute(&vault_path, cli.json, cli.tsv)
        }

        Commands::Run { issue_id, agent, model, verbose } => {
            let store = FileStore::new(&vault_path)
                .context("failed to open vault")?;
            let orch = Orchestrator::new(store);
            
            let agent_name = agent.as_deref().unwrap_or("claude");
            let run = orch.start_run(&issue_id, agent_name, model.as_deref())
                .context("failed to start run")?;
            
            if verbose {
                println!("Started run: {}#{}", run.issue_id, run.run_id);
                println!("Short ID: {}", run.short_id());
                println!("Tmux session: {}", run.tmux_session);
                println!("Worktree: {}", run.worktree_path);
            } else {
                println!("{}", run.short_id());
            }
            Ok(())
        }

        Commands::Continue { issue_ref, branch } => {
            let store = FileStore::new(&vault_path)
                .context("failed to open vault")?;
            let orch = Orchestrator::new(store);
            
            let run = orch.continue_run(&issue_ref, branch.as_deref())
                .context("failed to continue run")?;
            
            println!("{}", run.short_id());
            Ok(())
        }

        Commands::Attach { run_ref } => {
            let store = FileStore::new(&vault_path)
                .context("failed to open vault")?;
            let orch = Orchestrator::new(store);
            
            orch.attach(&run_ref)
                .context("failed to attach to run")
        }

        Commands::Stop { run_ref, all } => {
            let store = FileStore::new(&vault_path)
                .context("failed to open vault")?;
            let orch = Orchestrator::new(store);
            
            if all {
                let stopped = orch.stop_all()
                    .context("failed to stop runs")?;
                println!("Stopped {} runs", stopped);
            } else if let Some(ref_str) = run_ref {
                orch.stop(&ref_str)
                    .context("failed to stop run")?;
                println!("Stopped");
            } else {
                bail!("specify a run reference or --all");
            }
            Ok(())
        }

        Commands::Resolve { issue_id } => {
            let store = FileStore::new(&vault_path)
                .context("failed to open vault")?;
            
            store.set_issue_status(&issue_id, IssueStatus::Resolved)
                .context("failed to set issue status")?;
            
            println!("Issue '{}' marked as resolved", issue_id);
            Ok(())
        }

        Commands::Open { issue_id } => {
            let store = FileStore::new(&vault_path)
                .context("failed to open vault")?;
            
            let issue = store.resolve_issue(&issue_id)
                .context("failed to resolve issue")?;
            
            let editor = std::env::var("EDITOR").unwrap_or_else(|_| "vim".to_string());
            let status = std::process::Command::new(&editor)
                .arg(&issue.path)
                .status()
                .context("failed to run editor")?;
            
            if !status.success() {
                bail!("editor exited with error");
            }
            Ok(())
        }

        Commands::Monitor => {
            let status = std::process::Command::new("orch-monitor")
                .args(["--vault", vault_path.to_str().unwrap_or_default()])
                .status()
                .context("failed to run monitor")?;
            
            if !status.success() {
                bail!("monitor exited with error");
            }
            Ok(())
        }

        Commands::Daemon { stop, restart } => {
            use crate::daemon;

            if stop {
                daemon::kill(&vault_path)
                    .context("failed to stop daemon")?;
                println!("Daemon stopped");
            } else if restart {
                daemon::kill(&vault_path)
                    .context("failed to stop daemon")?;
                println!("Daemon restarted (note: full daemon implementation pending)");
            } else {
                if daemon::is_running(&vault_path) {
                    if let Some(pid) = daemon::get_running_pid(&vault_path) {
                        println!("Daemon is running (PID: {})", pid);
                    } else {
                        println!("Daemon is running");
                    }
                } else {
                    println!("Daemon is not running");
                }
            }
            Ok(())
        }

        Commands::Repair => {
            use crate::daemon;

            if !daemon::is_running(&vault_path) {
                let _ = daemon::remove_pid(&vault_path);
                println!("Cleaned up stale daemon PID file");
            }
            println!("Repair completed");
            Ok(())
        }

        Commands::Delete { run_ref, force } => {
            let store = FileStore::new(&vault_path)
                .context("failed to open vault")?;
            let orch = Orchestrator::new(store);
            
            orch.delete_run(&run_ref, force)
                .context("failed to delete run")?;
            
            println!("Deleted");
            Ok(())
        }

        Commands::Exec { run_ref, command } => {
            let store = FileStore::new(&vault_path)
                .context("failed to open vault")?;
            let orch = Orchestrator::new(store);
            
            let status = orch.exec_in_worktree(&run_ref, &command)
                .context("failed to execute command")?;
            
            std::process::exit(status.code().unwrap_or(1));
        }

        Commands::Send { run_ref, input } => {
            let store = FileStore::new(&vault_path)
                .context("failed to open vault")?;
            let orch = Orchestrator::new(store);
            
            orch.send_input(&run_ref, &input)
                .context("failed to send input")?;
            Ok(())
        }

        Commands::Capture { run_ref, output } => {
            let store = FileStore::new(&vault_path)
                .context("failed to open vault")?;
            let orch = Orchestrator::new(store);
            
            let captured = orch.capture_output(&run_ref)
                .context("failed to capture output")?;
            
            if let Some(path) = output {
                std::fs::write(&path, &captured)
                    .context("failed to write output file")?;
                println!("Captured to {}", path.display());
            } else {
                print!("{}", captured);
            }
            Ok(())
        }

        Commands::CaptureAll { output } => {
            let store = FileStore::new(&vault_path)
                .context("failed to open vault")?;
            let orch = Orchestrator::new(store);
            
            let count = orch.capture_all(output.as_deref())
                .context("failed to capture all")?;
            
            println!("Captured {} runs", count);
            Ok(())
        }

        Commands::Models => {
            println!("Available agents:");
            println!("  claude    - Anthropic Claude CLI");
            println!("  codex     - OpenAI Codex CLI");
            println!("  gemini    - Google Gemini CLI");
            println!("  opencode  - OpenCode CLI");
            Ok(())
        }
    }
}
