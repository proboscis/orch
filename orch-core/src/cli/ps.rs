//! ps command - list runs

use anyhow::{Context, Result};
use clap::Args;
use std::path::Path;

use crate::models::Status;
use crate::store::{FileStore, ListRunsFilter, Store};

#[derive(Args, Debug)]
pub struct PsCommand {
    /// Issue ID to filter by
    #[arg(long, short)]
    pub issue: Option<String>,

    /// Status to filter by
    #[arg(long, short)]
    pub status: Option<String>,

    /// Only show active runs
    #[arg(long, short)]
    pub active: bool,

    /// Limit number of results
    #[arg(long, short, default_value = "20")]
    pub limit: usize,

    /// Show all runs (no limit)
    #[arg(long)]
    pub all: bool,
}

impl PsCommand {
    pub fn execute(&self, vault_path: &Path, json: bool, tsv: bool) -> Result<()> {
        let store = FileStore::new(vault_path)
            .context("failed to open vault")?;

        let mut filter = ListRunsFilter::default();
        
        if let Some(ref issue_id) = self.issue {
            filter.issue_id = Some(issue_id.clone());
        }

        if self.active {
            filter.status = vec![
                Status::Queued,
                Status::Booting,
                Status::Running,
                Status::Blocked,
                Status::BlockedApi,
                Status::PrOpen,
                Status::Unknown,
            ];
        } else if let Some(ref status_str) = self.status {
            if let Ok(status) = status_str.parse() {
                filter.status = vec![status];
            }
        }

        if !self.all {
            filter.limit = Some(self.limit);
        }

        let runs = store.list_runs(&filter)
            .context("failed to list runs")?;

        if json {
            println!("{}", serde_json::to_string_pretty(&runs)
                .context("failed to serialize runs")?);
        } else if tsv {
            // TSV format for fzf
            for run in &runs {
                println!(
                    "{}\t{}\t{}\t{}\t{}",
                    run.short_id(),
                    run.issue_id,
                    run.run_id,
                    run.status,
                    run.agent
                );
            }
        } else {
            // Human-readable format
            if runs.is_empty() {
                println!("No runs found.");
            } else {
                println!(
                    "{:<6} {:<15} {:<17} {:<10} {:<8} {:<10}",
                    "ID", "ISSUE", "RUN", "STATUS", "ELAPSED", "AGENT"
                );
                println!("{}", "-".repeat(70));

                for run in &runs {
                    println!(
                        "{:<6} {:<15} {:<17} {:<10} {:<8} {:<10}",
                        run.short_id(),
                        truncate(&run.issue_id, 15),
                        truncate(&run.run_id, 17),
                        run.status,
                        run.elapsed_time(),
                        truncate(&run.agent, 10)
                    );
                }
            }
        }

        Ok(())
    }
}

fn truncate(s: &str, max: usize) -> String {
    if s.len() > max {
        format!("{}...", &s[..max.saturating_sub(3)])
    } else {
        s.to_string()
    }
}
