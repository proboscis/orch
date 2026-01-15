//! File-based store implementation.

use chrono::{DateTime, Utc};
use std::collections::HashMap;
use std::fs::{self, OpenOptions};
use std::io::{BufWriter, Write};
use std::path::{Path, PathBuf};
use std::sync::{Arc, RwLock};
use walkdir::WalkDir;

use super::{ListRunsFilter, Store, StoreError};
use crate::models::{Event, Issue, IssueStatus, Run, RunRef, Status};

/// File-based store implementation.
#[derive(Debug)]
pub struct FileStore {
    vault_path: PathBuf,
    issue_cache: Arc<RwLock<HashMap<String, Issue>>>,
    cache_dirty: Arc<RwLock<bool>>,
}

impl FileStore {
    /// Create a new FileStore.
    pub fn new(vault_path: impl AsRef<Path>) -> Result<Self, StoreError> {
        let vault_path = vault_path.as_ref().canonicalize()
            .map_err(|_| StoreError::VaultNotFound(vault_path.as_ref().to_path_buf()))?;

        if !vault_path.is_dir() {
            return Err(StoreError::VaultNotDirectory(vault_path));
        }

        Ok(Self {
            vault_path,
            issue_cache: Arc::new(RwLock::new(HashMap::new())),
            cache_dirty: Arc::new(RwLock::new(true)),
        })
    }

    /// Get the path to a run document.
    fn run_path(&self, issue_id: &str, run_id: &str) -> PathBuf {
        self.vault_path.join("runs").join(issue_id).join(format!("{}.md", run_id))
    }

    /// Get the path to the runs directory for an issue.
    fn runs_dir(&self, issue_id: &str) -> PathBuf {
        self.vault_path.join("runs").join(issue_id)
    }

    /// Scan vault and find all files with type: issue frontmatter.
    fn scan_issues(&self) -> Result<(), StoreError> {
        let runs_dir = self.vault_path.join("runs");
        let mut issues = HashMap::new();

        for entry in WalkDir::new(&self.vault_path)
            .follow_links(true)
            .into_iter()
            .filter_entry(|e| {
                // Skip runs directory
                e.path() != runs_dir
            })
            .filter_map(|e| e.ok())
        {
            let path = entry.path();
            
            // Skip directories and non-markdown files
            if path.is_dir() || path.extension().map_or(true, |ext| ext != "md") {
                continue;
            }

            // Try to parse as issue
            if let Some(issue) = self.parse_issue_file(path)? {
                issues.insert(issue.id.clone(), issue);
            }
        }

        let mut cache = self.issue_cache.write().unwrap();
        *cache = issues;
        let mut dirty = self.cache_dirty.write().unwrap();
        *dirty = false;

        Ok(())
    }

    /// Parse a file and return an Issue if it has type: issue frontmatter.
    fn parse_issue_file(&self, path: &Path) -> Result<Option<Issue>, StoreError> {
        let content = fs::read_to_string(path)?;
        let lines: Vec<&str> = content.lines().collect();

        if lines.is_empty() || lines[0].trim() != "---" {
            return Ok(None); // No frontmatter
        }

        let mut frontmatter = HashMap::new();
        let mut body_start = 0;

        for (i, line) in lines.iter().enumerate().skip(1) {
            if line.trim() == "---" {
                body_start = i + 1;
                break;
            }
            if let Some((key, value)) = line.split_once(':') {
                frontmatter.insert(
                    key.trim().to_string(),
                    value.trim().to_string(),
                );
            }
        }

        // Check if this is an issue file
        if frontmatter.get("type").map(|s| s.as_str()) != Some("issue") {
            return Ok(None);
        }

        // Get issue ID from frontmatter or filename
        let issue_id = frontmatter
            .get("id")
            .cloned()
            .unwrap_or_else(|| {
                path.file_stem()
                    .map(|s| s.to_string_lossy().to_string())
                    .unwrap_or_default()
            });

        // Get title
        let mut title = frontmatter.get("title").cloned().unwrap_or_default();
        if title.is_empty() {
            for line in lines.iter().skip(body_start) {
                if line.starts_with("# ") {
                    title = line.trim_start_matches("# ").to_string();
                    break;
                }
            }
        }

        // Get body
        let body = if body_start < lines.len() {
            lines[body_start..].join("\n")
        } else {
            String::new()
        };

        // Get topic and summary
        let topic = frontmatter.get("topic").cloned().unwrap_or_default();
        let mut summary = frontmatter.get("summary").cloned().unwrap_or_default();
        if summary.is_empty() && !title.is_empty() {
            summary = if title.len() > 50 {
                format!("{}...", &title[..47])
            } else {
                title.clone()
            };
        }

        // Get status
        let status = frontmatter
            .get("status")
            .map(|s| s.parse().unwrap_or_default())
            .unwrap_or_default();

        Ok(Some(Issue {
            id: issue_id,
            title,
            topic,
            summary,
            status,
            body,
            path: path.to_path_buf(),
            frontmatter,
        }))
    }

    /// Load a run from its file.
    fn load_run(&self, issue_id: &str, run_id: &str, path: &Path) -> Result<Run, StoreError> {
        let content = fs::read_to_string(path)?;

        let mut run = Run {
            issue_id: issue_id.to_string(),
            run_id: run_id.to_string(),
            path: path.to_path_buf(),
            ..Default::default()
        };

        // Parse frontmatter
        let lines: Vec<&str> = content.lines().collect();
        let mut body_start = 0;

        if !lines.is_empty() && lines[0].trim() == "---" {
            for (i, line) in lines.iter().enumerate().skip(1) {
                if line.trim() == "---" {
                    body_start = i + 1;
                    break;
                }
                if let Some((key, value)) = line.split_once(':') {
                    let key = key.trim();
                    let value = value.trim();
                    match key {
                        "agent" => run.agent = value.to_string(),
                        "model" => run.model = value.to_string(),
                        "model_variant" => run.model_variant = value.to_string(),
                        "continued_from" => run.continued_from = value.to_string(),
                        _ => {}
                    }
                }
            }
        }

        // Parse events from body
        for line in lines.iter().skip(body_start) {
            if line.starts_with("- ") && line.contains('|') {
                if let Some(event) = Event::parse(line) {
                    run.events.push(event);
                }
            }
        }

        run.derive_state();

        // Resolve relative worktree paths against the vault path
        if !run.worktree_path.is_empty() && !PathBuf::from(&run.worktree_path).is_absolute() {
            run.worktree_path = self.vault_path.join(&run.worktree_path).to_string_lossy().to_string();
        }

        Ok(run)
    }

    fn is_cache_dirty(&self) -> bool {
        *self.cache_dirty.read().unwrap()
    }

    fn mark_cache_dirty(&self) {
        *self.cache_dirty.write().unwrap() = true;
    }
}

impl Store for FileStore {
    fn resolve_issue(&self, issue_id: &str) -> Result<Issue, StoreError> {
        if self.is_cache_dirty() {
            self.scan_issues()?;
        }

        let cache = self.issue_cache.read().unwrap();
        if let Some(issue) = cache.get(issue_id) {
            return Ok(issue.clone());
        }
        drop(cache);

        // Try rescanning in case file was added
        self.mark_cache_dirty();
        self.scan_issues()?;

        let cache = self.issue_cache.read().unwrap();
        cache
            .get(issue_id)
            .cloned()
            .ok_or_else(|| StoreError::IssueNotFound(issue_id.to_string()))
    }

    fn list_issues(&self) -> Result<Vec<Issue>, StoreError> {
        // Always rescan issues from disk to ensure fresh data
        self.scan_issues()?;

        let cache = self.issue_cache.read().unwrap();
        Ok(cache.values().cloned().collect())
    }

    fn set_issue_status(&self, issue_id: &str, status: IssueStatus) -> Result<(), StoreError> {
        let issue = self.resolve_issue(issue_id)?;
        let content = fs::read_to_string(&issue.path)?;

        let lines: Vec<&str> = content.lines().collect();
        if lines.is_empty() || lines[0].trim() != "---" {
            return Err(StoreError::Parse(format!(
                "issue file has no frontmatter: {}",
                issue.path.display()
            )));
        }

        let status_str = status.to_string();
        let mut new_lines = vec![lines[0].to_string()];
        let mut found_status = false;
        let mut in_frontmatter = true;

        for line in lines.iter().skip(1) {
            if in_frontmatter {
                if line.trim() == "---" {
                    if !found_status {
                        new_lines.push(format!("status: {}", status_str));
                    }
                    new_lines.push(line.to_string());
                    in_frontmatter = false;
                    continue;
                }

                if let Some((key, _)) = line.split_once(':') {
                    if key.trim() == "status" {
                        new_lines.push(format!("status: {}", status_str));
                        found_status = true;
                    } else {
                        new_lines.push(line.to_string());
                    }
                } else {
                    new_lines.push(line.to_string());
                }
            } else {
                new_lines.push(line.to_string());
            }
        }

        fs::write(&issue.path, new_lines.join("\n"))?;
        self.mark_cache_dirty();

        Ok(())
    }

    fn create_run(
        &self,
        issue_id: &str,
        run_id: &str,
        metadata: HashMap<String, String>,
    ) -> Result<Run, StoreError> {
        // Verify issue exists
        let _ = self.resolve_issue(issue_id)?;

        // Create runs directory for issue if needed
        let runs_dir = self.runs_dir(issue_id);
        fs::create_dir_all(&runs_dir)?;

        // Create run document
        let run_path = self.run_path(issue_id, run_id);
        if run_path.exists() {
            return Err(StoreError::RunAlreadyExists(
                issue_id.to_string(),
                run_id.to_string(),
            ));
        }

        // Build frontmatter
        let mut content = String::new();
        content.push_str("---\n");
        content.push_str(&format!("issue: {}\n", issue_id));
        content.push_str(&format!("run: {}\n", run_id));
        content.push_str(&format!("created: {}\n", Utc::now().format("%Y-%m-%dT%H:%M:%S%.3fZ")));
        for (key, value) in &metadata {
            content.push_str(&format!("{}: {}\n", key, value));
        }
        content.push_str("---\n\n");
        content.push_str("# Events\n\n");

        fs::write(&run_path, content)?;

        Ok(Run {
            issue_id: issue_id.to_string(),
            run_id: run_id.to_string(),
            path: run_path,
            status: Status::Queued,
            events: Vec::new(),
            started_at: Some(Utc::now()),
            updated_at: Some(Utc::now()),
            ..Default::default()
        })
    }

    fn append_event(&self, run_ref: &RunRef, event: &Event) -> Result<(), StoreError> {
        let run = self.get_run(run_ref)?;

        let file = OpenOptions::new()
            .append(true)
            .open(&run.path)?;
        let mut writer = BufWriter::new(file);

        writeln!(writer, "{}", event.to_line())?;
        writer.flush()?;
        // Sync ensures daemon writes are immediately visible to monitor
        writer.get_ref().sync_all()?;

        Ok(())
    }

    fn list_runs(&self, filter: &ListRunsFilter) -> Result<Vec<Run>, StoreError> {
        let mut runs = Vec::new();
        let runs_root = self.vault_path.join("runs");

        // Get list of issue directories
        let issue_dirs: Vec<String> = if let Some(ref issue_id) = filter.issue_id {
            vec![issue_id.clone()]
        } else {
            match fs::read_dir(&runs_root) {
                Ok(entries) => entries
                    .filter_map(|e| e.ok())
                    .filter(|e| e.path().is_dir())
                    .filter_map(|e| e.file_name().to_str().map(|s| s.to_string()))
                    .collect(),
                Err(_) => return Ok(runs),
            }
        };

        // Parse since filter
        let since_time: Option<DateTime<Utc>> = filter.since.as_ref().and_then(|s| {
            DateTime::parse_from_rfc3339(s)
                .ok()
                .map(|dt| dt.with_timezone(&Utc))
        });

        // Status filter set
        let status_filter: std::collections::HashSet<_> = filter.status.iter().collect();

        // Load runs from each issue directory
        for issue_id in issue_dirs {
            let issue_runs_dir = runs_root.join(&issue_id);
            let entries = match fs::read_dir(&issue_runs_dir) {
                Ok(e) => e,
                Err(_) => continue,
            };

            for entry in entries.filter_map(|e| e.ok()) {
                let path = entry.path();
                if path.is_dir() || path.extension().map_or(true, |ext| ext != "md") {
                    continue;
                }

                let run_id = path.file_stem()
                    .and_then(|s| s.to_str())
                    .unwrap_or_default();

                let run = match self.load_run(&issue_id, run_id, &path) {
                    Ok(r) => r,
                    Err(_) => continue,
                };

                // Apply filters
                if !status_filter.is_empty() && !status_filter.contains(&run.status) {
                    continue;
                }
                if let (Some(since), Some(updated)) = (since_time, run.updated_at) {
                    if updated < since {
                        continue;
                    }
                }

                runs.push(run);
            }
        }

        // Sort by updated_at descending
        runs.sort_by(|a, b| {
            let a_time = a.updated_at.unwrap_or_default();
            let b_time = b.updated_at.unwrap_or_default();
            b_time.cmp(&a_time)
        });

        // Apply limit
        if let Some(limit) = filter.limit {
            runs.truncate(limit);
        }

        Ok(runs)
    }

    fn get_run(&self, run_ref: &RunRef) -> Result<Run, StoreError> {
        if run_ref.is_latest() {
            return self.get_latest_run(&run_ref.issue_id);
        }

        let run_path = self.run_path(&run_ref.issue_id, &run_ref.run_id);
        if !run_path.exists() {
            return Err(StoreError::RunNotFound(run_ref.to_string()));
        }

        self.load_run(&run_ref.issue_id, &run_ref.run_id, &run_path)
    }

    fn get_run_by_short_id(&self, short_id: &str) -> Result<Run, StoreError> {
        let runs = self.list_runs(&ListRunsFilter::default())?;

        let matches: Vec<_> = runs
            .into_iter()
            .filter(|run| run.short_id().starts_with(short_id))
            .collect();

        match matches.len() {
            0 => Err(StoreError::RunNotFound(short_id.to_string())),
            1 => Ok(matches.into_iter().next().unwrap()),
            n => Err(StoreError::AmbiguousRunId(short_id.to_string(), n)),
        }
    }

    fn get_latest_run(&self, issue_id: &str) -> Result<Run, StoreError> {
        let runs_dir = self.runs_dir(issue_id);
        let entries = fs::read_dir(&runs_dir)
            .map_err(|_| StoreError::RunNotFound(format!("no runs for issue: {}", issue_id)))?;

        let mut latest_name: Option<String> = None;
        for entry in entries.filter_map(|e| e.ok()) {
            let path = entry.path();
            if path.is_dir() || path.extension().map_or(true, |ext| ext != "md") {
                continue;
            }

            if let Some(name) = path.file_stem().and_then(|s| s.to_str()) {
                if latest_name.as_ref().map_or(true, |latest| name > latest.as_str()) {
                    latest_name = Some(name.to_string());
                }
            }
        }

        let latest = latest_name
            .ok_or_else(|| StoreError::RunNotFound(format!("no runs for issue: {}", issue_id)))?;

        self.load_run(issue_id, &latest, &self.run_path(issue_id, &latest))
    }

    fn vault_path(&self) -> &Path {
        &self.vault_path
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    fn setup_test_vault() -> (TempDir, FileStore) {
        let temp_dir = TempDir::new().unwrap();
        let vault_path = temp_dir.path();

        // Create issues directory with a test issue
        let issues_dir = vault_path.join("issues");
        fs::create_dir_all(&issues_dir).unwrap();
        
        let issue_content = r#"---
type: issue
id: test-issue
title: Test Issue
status: open
---

# Test Issue

This is a test issue body.
"#;
        fs::write(issues_dir.join("test-issue.md"), issue_content).unwrap();

        // Create runs directory
        fs::create_dir_all(vault_path.join("runs")).unwrap();

        let store = FileStore::new(vault_path).unwrap();
        (temp_dir, store)
    }

    #[test]
    fn test_resolve_issue() {
        let (_temp, store) = setup_test_vault();
        
        let issue = store.resolve_issue("test-issue").unwrap();
        assert_eq!(issue.id, "test-issue");
        assert_eq!(issue.title, "Test Issue");
        assert_eq!(issue.status, IssueStatus::Open);
    }

    #[test]
    fn test_list_issues() {
        let (_temp, store) = setup_test_vault();
        
        let issues = store.list_issues().unwrap();
        assert_eq!(issues.len(), 1);
        assert_eq!(issues[0].id, "test-issue");
    }

    #[test]
    fn test_create_and_get_run() {
        let (_temp, store) = setup_test_vault();
        
        let metadata = HashMap::new();
        let run = store.create_run("test-issue", "20240115-100000", metadata).unwrap();
        
        assert_eq!(run.issue_id, "test-issue");
        assert_eq!(run.run_id, "20240115-100000");
        assert_eq!(run.status, Status::Queued);

        let run_ref = RunRef::parse("test-issue#20240115-100000").unwrap();
        let fetched = store.get_run(&run_ref).unwrap();
        assert_eq!(fetched.issue_id, "test-issue");
    }

    #[test]
    fn test_append_event() {
        let (_temp, store) = setup_test_vault();
        
        let metadata = HashMap::new();
        store.create_run("test-issue", "20240115-100000", metadata).unwrap();
        
        let run_ref = RunRef::parse("test-issue#20240115-100000").unwrap();
        let event = Event::status(Status::Running);
        store.append_event(&run_ref, &event).unwrap();

        let run = store.get_run(&run_ref).unwrap();
        assert_eq!(run.status, Status::Running);
    }
}
