//! show command - show details of a run or issue

use anyhow::{Context, Result, bail};
use clap::Args;
use std::path::Path;

use crate::models::RunRef;
use crate::store::{FileStore, Store};

#[derive(Args, Debug)]
pub struct ShowCommand {
    /// Reference to show (issue ID, run ref, or short ID)
    pub reference: String,

    /// Show full events log
    #[arg(long, short)]
    pub events: bool,
}

/// Check if a string looks like a short ID (2-6 hex chars, case insensitive)
fn is_short_id(s: &str) -> bool {
    s.len() >= 2 && s.len() <= 6 && s.chars().all(|c| c.is_ascii_hexdigit())
}

impl ShowCommand {
    pub fn execute(&self, vault_path: &Path, json: bool, _tsv: bool) -> Result<()> {
        let store = FileStore::new(vault_path)
            .context("failed to open vault")?;

        // Normalize short ID to lowercase for matching
        let reference = self.reference.to_ascii_lowercase();

        // Try to determine if this is a run reference or issue ID
        // First, try as a short ID (case insensitive)
        if is_short_id(&reference) {
            match store.get_run_by_short_id(&reference) {
                Ok(run) => {
                    return self.show_run(&run, json);
                }
                Err(_) if reference.len() < 6 => {
                    // Could be a prefix that didn't match, try as issue
                }
                Err(e) => return Err(e).context("failed to find run by short ID"),
            }
        }

        // Try as a run reference
        if let Ok(run_ref) = RunRef::parse(&self.reference) {
            match store.get_run(&run_ref) {
                Ok(run) => {
                    return self.show_run(&run, json);
                }
                Err(_) => {
                    // Try as issue if run ref failed
                }
            }
        }

        // Try as an issue ID
        match store.resolve_issue(&self.reference) {
            Ok(issue) => {
                if json {
                    println!("{}", serde_json::to_string_pretty(&issue)
                        .context("failed to serialize issue")?);
                } else {
                    println!("Issue: {}", issue.id);
                    println!("Title: {}", issue.title);
                    println!("Status: {}", issue.status);
                    if !issue.topic.is_empty() {
                        println!("Topic: {}", issue.topic);
                    }
                    println!("Path: {}", issue.path.display());
                    println!();
                    println!("--- Body ---");
                    println!("{}", issue.body);
                }
                Ok(())
            }
            Err(e) => bail!("not found: {} ({})", self.reference, e),
        }
    }

    fn show_run(&self, run: &crate::models::Run, json: bool) -> Result<()> {
        if json {
            println!("{}", serde_json::to_string_pretty(&run)
                .context("failed to serialize run")?);
        } else {
            println!("Run: {}#{}", run.issue_id, run.run_id);
            println!("Short ID: {}", run.short_id());
            println!("Status: {}", run.status);
            if let Some(ref phase) = run.phase {
                println!("Phase: {}", phase);
            }
            println!("Elapsed: {}", run.elapsed_time());
            println!();

            if !run.agent.is_empty() {
                println!("Agent: {}", run.agent);
            }
            if !run.model.is_empty() {
                println!("Model: {}", run.model);
            }
            if !run.branch.is_empty() {
                println!("Branch: {}", run.branch);
            }
            if !run.worktree_path.is_empty() {
                println!("Worktree: {}", run.worktree_path);
            }
            if !run.tmux_session.is_empty() {
                println!("Tmux Session: {}", run.tmux_session);
            }
            if !run.pr_url.is_empty() {
                println!("PR: {}", run.pr_url);
            }
            if run.server_port > 0 {
                println!("Server Port: {}", run.server_port);
            }
            println!("Path: {}", run.path.display());

            if self.events && !run.events.is_empty() {
                println!();
                println!("--- Events ---");
                for event in &run.events {
                    println!("{}", event.to_line());
                }
            }
        }
        Ok(())
    }
}
