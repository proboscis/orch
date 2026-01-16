use anyhow::{Context, Result};
use clap::Args;
use std::path::Path;

use crate::agent::{check_alive_batch, AliveStatus};
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

        let alive_statuses = check_alive_batch(&runs);

        if json {
            #[derive(serde::Serialize)]
            struct RunWithAlive<'a> {
                #[serde(flatten)]
                run: &'a crate::models::Run,
                alive: String,
            }

            let runs_with_alive: Vec<RunWithAlive> = runs.iter()
                .map(|run| {
                    let ref_key = format!("{}#{}", run.issue_id, run.run_id);
                    let alive = alive_statuses.get(&ref_key)
                        .unwrap_or(&AliveStatus::Unknown)
                        .to_string();
                    RunWithAlive { run, alive }
                })
                .collect();

            println!("{}", serde_json::to_string_pretty(&runs_with_alive)
                .context("failed to serialize runs")?);
        } else if tsv {
            for run in &runs {
                let ref_key = format!("{}#{}", run.issue_id, run.run_id);
                let alive = alive_statuses.get(&ref_key)
                    .unwrap_or(&AliveStatus::Unknown);
                println!(
                    "{}\t{}\t{}\t{}\t{}\t{}",
                    run.short_id(),
                    run.issue_id,
                    run.run_id,
                    run.status,
                    run.agent,
                    alive
                );
            }
        } else {
            if runs.is_empty() {
                println!("No runs found.");
            } else {
                println!(
                    "{:<6} {:<15} {:<17} {:<10} {:<5} {:<8} {:<10}",
                    "ID", "ISSUE", "RUN", "STATUS", "ALIVE", "ELAPSED", "AGENT"
                );
                println!("{}", "-".repeat(77));

                for run in &runs {
                    let ref_key = format!("{}#{}", run.issue_id, run.run_id);
                    let alive = alive_statuses.get(&ref_key)
                        .unwrap_or(&AliveStatus::Unknown);
                    println!(
                        "{:<6} {:<15} {:<17} {:<10} {:<5} {:<8} {:<10}",
                        run.short_id(),
                        truncate(&run.issue_id, 15),
                        truncate(&run.run_id, 17),
                        run.status,
                        alive,
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
