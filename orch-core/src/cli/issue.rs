//! issue command - list issues

use clap::Args;
use crate::models::IssueStatus;
use crate::store::{FileStore, Store};
use std::path::Path;

#[derive(Args, Debug)]
pub struct IssueCommand {
    /// Include resolved issues
    #[arg(long, short)]
    pub resolved: bool,

    /// Include closed issues
    #[arg(long, short)]
    pub closed: bool,

    /// Show all issues (including resolved and closed)
    #[arg(long, short)]
    pub all: bool,
}

impl IssueCommand {
    pub fn execute(&self, vault_path: &Path, json: bool, tsv: bool) -> Result<(), String> {
        let store = FileStore::new(vault_path)
            .map_err(|e| e.to_string())?;

        let issues = store.list_issues()
            .map_err(|e| e.to_string())?;

        // Filter issues based on flags
        let issues: Vec<_> = if self.all {
            issues
        } else {
            issues.into_iter()
                .filter(|i| {
                    match i.status {
                        IssueStatus::Open => true,
                        IssueStatus::Resolved => self.resolved,
                        IssueStatus::Closed => self.closed,
                    }
                })
                .collect()
        };

        if json {
            println!("{}", serde_json::to_string_pretty(&issues)
                .map_err(|e| e.to_string())?);
        } else if tsv {
            // TSV format for fzf
            for issue in &issues {
                println!(
                    "{}\t{}\t{}",
                    issue.id,
                    issue.status,
                    issue.title
                );
            }
        } else {
            // Human-readable format
            if issues.is_empty() {
                println!("No issues found.");
            } else {
                println!(
                    "{:<20} {:<10} {}",
                    "ID", "STATUS", "TITLE"
                );
                println!("{}", "-".repeat(70));

                for issue in &issues {
                    println!(
                        "{:<20} {:<10} {}",
                        truncate(&issue.id, 20),
                        issue.status,
                        truncate(&issue.title, 40)
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
