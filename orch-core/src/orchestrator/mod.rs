//! Orchestrator - Coordinates run lifecycle operations.
//!
//! This module provides a high-level API for managing agent runs,
//! composing the store, git, tmux, and agent modules into workflows.

use anyhow::{Context, Result, bail};
use std::collections::HashMap;
use std::path::Path;
use std::process::{Command, ExitStatus};

use crate::agent::{AgentType, get_agent_command};
use crate::git;
use crate::models::{Event, Run, RunRef, Status};
use crate::models::run::{generate_run_id, generate_branch_name, generate_tmux_session, generate_worktree_name};
use crate::store::{ListRunsFilter, Store};
use crate::tmux;

/// Orchestrator for managing agent run lifecycles.
pub struct Orchestrator<S: Store> {
    store: S,
}

impl<S: Store> Orchestrator<S> {
    /// Create a new Orchestrator with the given store.
    pub fn new(store: S) -> Self {
        Self { store }
    }

    /// Get a reference to the underlying store.
    pub fn store(&self) -> &S {
        &self.store
    }

    /// Resolve a run reference (short ID, issue#run, or issue for latest).
    fn resolve_run(&self, run_ref: &str) -> Result<Run> {
        // Try short ID first (2-6 hex chars)
        if run_ref.len() <= 6 && run_ref.chars().all(|c| c.is_ascii_hexdigit()) {
            return self.store.get_run_by_short_id(run_ref)
                .map_err(|e| anyhow::anyhow!(e));
        }

        // Try as run reference
        let parsed = RunRef::parse(run_ref)
            .map_err(|e| anyhow::anyhow!(e))?;
        
        self.store.get_run(&parsed)
            .map_err(|e| anyhow::anyhow!(e))
    }

    /// Start a new run for an issue.
    pub fn start_run(&self, issue_id: &str, agent: &str, _model: Option<&str>) -> Result<Run> {
        // Verify issue exists
        let _issue = self.store.resolve_issue(issue_id)
            .context("issue not found")?;

        // Generate run ID and names
        let run_id = generate_run_id();
        let branch_name = generate_branch_name(issue_id, &run_id);
        let tmux_session = generate_tmux_session(issue_id, &run_id);
        let worktree_name = generate_worktree_name(issue_id, &run_id, agent);

        // Find repo root
        let repo_root = git::find_repo_root(".")
            .context("not in a git repository")?;

        // Create worktree directory
        let worktree_root = repo_root.join(".git-worktrees").join(issue_id);
        std::fs::create_dir_all(&worktree_root)
            .context("failed to create worktree root")?;
        
        let worktree_path = worktree_root.join(&worktree_name);

        // Fetch latest
        git::fetch(&repo_root)
            .context("failed to fetch")?;
        
        // Create worktree with new branch from origin/main
        git::create_worktree_with_new_branch(&repo_root, &worktree_path, &branch_name, "origin/main")
            .context("failed to create worktree")?;

        // Create run in store
        let mut metadata = HashMap::new();
        metadata.insert("agent".to_string(), agent.to_string());

        let mut run = self.store.create_run(issue_id, &run_id, metadata)
            .context("failed to create run")?;

        // Create tmux session
        let worktree_str = worktree_path.to_string_lossy();
        tmux::create_session(&tmux_session, Some(&worktree_str))
            .context("failed to create tmux session")?;

        // Record artifacts
        self.store.append_event(&run.run_ref(), &Event::artifact(
            "worktree",
            [("path".to_string(), worktree_path.to_string_lossy().to_string())].into_iter().collect(),
        )).ok();

        self.store.append_event(&run.run_ref(), &Event::artifact(
            "branch",
            [("name".to_string(), branch_name.clone())].into_iter().collect(),
        )).ok();

        self.store.append_event(&run.run_ref(), &Event::artifact(
            "session",
            [("name".to_string(), tmux_session.clone())].into_iter().collect(),
        )).ok();

        // Update run status
        self.store.append_event(&run.run_ref(), &Event::status(Status::Booting)).ok();

        // Get agent command and start it
        let agent_type: AgentType = agent.parse().unwrap_or(AgentType::Claude);
        let cmd_parts = get_agent_command(agent_type);
        
        if !cmd_parts.is_empty() {
            let cmd = cmd_parts.join(" ");
            tmux::send_keys(&tmux_session, &cmd, true)
                .context("failed to start agent")?;
            
            self.store.append_event(&run.run_ref(), &Event::status(Status::Running)).ok();
        }

        // Update run fields
        run.tmux_session = tmux_session;
        run.branch = branch_name;
        run.worktree_path = worktree_path.to_string_lossy().to_string();
        run.agent = agent.to_string();
        run.status = Status::Running;

        Ok(run)
    }

    /// Continue from an existing run or branch.
    pub fn continue_run(&self, issue_ref: &str, branch: Option<&str>) -> Result<Run> {
        // Parse the reference to get issue ID
        let parsed = RunRef::parse(issue_ref)
            .map_err(|e| anyhow::anyhow!(e))?;
        
        let issue_id = &parsed.issue_id;

        // Get branch to continue from
        let branch_name = if let Some(b) = branch {
            b.to_string()
        } else {
            // Get from existing run
            let existing_run = self.store.get_run(&parsed)
                .context("run not found")?;
            if existing_run.branch.is_empty() {
                bail!("existing run has no branch; specify --branch");
            }
            existing_run.branch
        };

        // Get agent from existing run
        let agent = if !parsed.is_latest() {
            let existing_run = self.store.get_run(&parsed)
                .context("run not found")?;
            if existing_run.agent.is_empty() { "claude".to_string() } else { existing_run.agent }
        } else {
            "claude".to_string()
        };

        // Generate new run ID
        let run_id = generate_run_id();
        let tmux_session = generate_tmux_session(issue_id, &run_id);
        let worktree_name = generate_worktree_name(issue_id, &run_id, &agent);

        // Find repo root
        let repo_root = git::find_repo_root(".")
            .context("not in a git repository")?;

        // Create worktree from existing branch
        let worktree_root = repo_root.join(".git-worktrees").join(issue_id);
        std::fs::create_dir_all(&worktree_root)
            .context("failed to create worktree root")?;
        
        let worktree_path = worktree_root.join(&worktree_name);
        
        git::create_worktree(&repo_root, &worktree_path, &branch_name)
            .context("failed to create worktree")?;

        // Create run in store
        let mut metadata = HashMap::new();
        metadata.insert("agent".to_string(), agent.clone());
        metadata.insert("continued_from".to_string(), issue_ref.to_string());

        let mut run = self.store.create_run(issue_id, &run_id, metadata)
            .context("failed to create run")?;

        // Create tmux session
        let worktree_str = worktree_path.to_string_lossy();
        tmux::create_session(&tmux_session, Some(&worktree_str))
            .context("failed to create tmux session")?;

        // Record artifacts
        self.store.append_event(&run.run_ref(), &Event::artifact(
            "worktree",
            [("path".to_string(), worktree_path.to_string_lossy().to_string())].into_iter().collect(),
        )).ok();

        self.store.append_event(&run.run_ref(), &Event::artifact(
            "session",
            [("name".to_string(), tmux_session.clone())].into_iter().collect(),
        )).ok();

        // Update run status
        self.store.append_event(&run.run_ref(), &Event::status(Status::Booting)).ok();

        // Start agent
        let agent_type: AgentType = agent.parse().unwrap_or(AgentType::Claude);
        let cmd_parts = get_agent_command(agent_type);
        
        if !cmd_parts.is_empty() {
            let cmd = cmd_parts.join(" ");
            tmux::send_keys(&tmux_session, &cmd, true)
                .context("failed to start agent")?;
            
            self.store.append_event(&run.run_ref(), &Event::status(Status::Running)).ok();
        }

        // Update run fields
        run.tmux_session = tmux_session;
        run.branch = branch_name;
        run.worktree_path = worktree_path.to_string_lossy().to_string();
        run.agent = agent;
        run.status = Status::Running;

        Ok(run)
    }

    /// Attach to a run's tmux session.
    pub fn attach(&self, run_ref: &str) -> Result<()> {
        let run = self.resolve_run(run_ref)?;
        
        if run.tmux_session.is_empty() {
            bail!("run has no tmux session");
        }

        let status = Command::new("tmux")
            .args(["attach-session", "-t", &run.tmux_session])
            .status()
            .context("failed to run tmux")?;

        if !status.success() {
            bail!("failed to attach to tmux session");
        }
        Ok(())
    }

    /// Stop a run.
    pub fn stop(&self, run_ref: &str) -> Result<()> {
        let run = self.resolve_run(run_ref)?;
        
        // Kill tmux session if it exists
        if !run.tmux_session.is_empty() {
            tmux::kill_session(&run.tmux_session).ok();
        }

        // Record canceled event
        self.store.append_event(&run.run_ref(), &Event::status(Status::Canceled))
            .map_err(|e| anyhow::anyhow!(e))?;

        Ok(())
    }

    /// Stop all active runs.
    pub fn stop_all(&self) -> Result<usize> {
        let active_statuses = vec![
            Status::Queued,
            Status::Booting,
            Status::Running,
            Status::Blocked,
            Status::BlockedApi,
        ];

        let runs = self.store.list_runs(&ListRunsFilter {
            status: active_statuses,
            ..Default::default()
        }).map_err(|e| anyhow::anyhow!(e))?;

        let mut stopped = 0;
        for run in runs {
            if !run.tmux_session.is_empty() {
                tmux::kill_session(&run.tmux_session).ok();
            }
            self.store.append_event(&run.run_ref(), &Event::status(Status::Canceled)).ok();
            stopped += 1;
        }

        Ok(stopped)
    }

    /// Delete a run.
    pub fn delete_run(&self, run_ref: &str, _force: bool) -> Result<()> {
        let run = self.resolve_run(run_ref)?;
        
        // Kill tmux session if running
        if !run.tmux_session.is_empty() {
            tmux::kill_session(&run.tmux_session).ok();
        }

        // Remove worktree if it exists
        if !run.worktree_path.is_empty() {
            let worktree_path = Path::new(&run.worktree_path);
            if worktree_path.exists() {
                // Try to remove worktree properly
                let repo_root = git::find_repo_root(".").ok();
                if let Some(root) = repo_root {
                    Command::new("git")
                        .args(["worktree", "remove", "--force", &run.worktree_path])
                        .current_dir(&root)
                        .status()
                        .ok();
                }
            }
        }

        // Remove run file
        if run.path.exists() {
            std::fs::remove_file(&run.path)
                .context("failed to remove run file")?;
        }

        Ok(())
    }

    /// Execute a command in a run's worktree.
    pub fn exec_in_worktree(&self, run_ref: &str, command: &[String]) -> Result<ExitStatus> {
        let run = self.resolve_run(run_ref)?;
        
        if run.worktree_path.is_empty() {
            bail!("run has no worktree");
        }

        let worktree = Path::new(&run.worktree_path);
        if !worktree.exists() {
            bail!("worktree does not exist: {}", run.worktree_path);
        }

        if command.is_empty() {
            bail!("no command specified");
        }

        let status = Command::new(&command[0])
            .args(&command[1..])
            .current_dir(worktree)
            .status()
            .context("failed to execute command")?;

        Ok(status)
    }

    /// Send input to a run's tmux session.
    pub fn send_input(&self, run_ref: &str, input: &str) -> Result<()> {
        let run = self.resolve_run(run_ref)?;
        
        if run.tmux_session.is_empty() {
            bail!("run has no tmux session");
        }

        tmux::send_keys(&run.tmux_session, input, true)
            .context("failed to send input")
    }

    /// Capture output from a run's tmux session.
    pub fn capture_output(&self, run_ref: &str) -> Result<String> {
        let run = self.resolve_run(run_ref)?;
        
        if run.tmux_session.is_empty() {
            bail!("run has no tmux session");
        }

        tmux::capture_pane(&run.tmux_session)
            .context("failed to capture output")
    }

    /// Capture output from all active runs.
    pub fn capture_all(&self, output_dir: Option<&Path>) -> Result<usize> {
        let runs = self.store.list_runs(&ListRunsFilter::default())
            .map_err(|e| anyhow::anyhow!(e))?;

        let output_dir = output_dir.map(|p| p.to_path_buf())
            .unwrap_or_else(|| std::env::current_dir().unwrap_or_default());

        std::fs::create_dir_all(&output_dir)
            .context("failed to create output directory")?;

        let mut captured = 0;
        for run in runs {
            if run.tmux_session.is_empty() {
                continue;
            }

            if let Ok(output) = tmux::capture_pane(&run.tmux_session) {
                let filename = format!("{}_{}.txt", run.issue_id, run.run_id);
                let path = output_dir.join(&filename);
                std::fs::write(&path, &output).ok();
                captured += 1;
            }
        }

        Ok(captured)
    }
}
