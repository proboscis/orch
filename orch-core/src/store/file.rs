//! File-based store implementation.
//!
//! This implementation reads issues from `vault/*.md` files with `type: issue` frontmatter,
//! and runs from `vault/runs/<issue_id>/<run_id>.md` files.

use std::collections::HashMap;
use std::fs::{self, OpenOptions};
use std::io::{BufWriter, Write};
use std::path::{Path, PathBuf};

use chrono::{DateTime, Utc};
use serde::Deserialize;

use super::{ListRunsFilter, Store, StoreError};
use crate::models::{Event, Issue, IssueStatus, Run, RunRef, Status};
use crate::models::run::generate_short_id;

/// Frontmatter structure for YAML parsing.
#[derive(Debug, Deserialize, Default)]
struct Frontmatter {
    #[serde(rename = "type")]
    doc_type: Option<String>,
    id: Option<String>,
    title: Option<String>,
    topic: Option<String>,
    summary: Option<String>,
    status: Option<String>,
    // Run-specific fields
    agent: Option<String>,
    model: Option<String>,
    model_variant: Option<String>,
    continued_from: Option<String>,
    // Capture all other fields
    #[serde(flatten)]
    extra: HashMap<String, serde_yaml::Value>,
}

/// File-based store implementation.
#[derive(Debug)]
pub struct FileStore {
    vault_path: PathBuf,
}

impl FileStore {
    /// Create a new FileStore.
    pub fn new(vault_path: impl AsRef<Path>) -> Result<Self, StoreError> {
        let vault_path = vault_path.as_ref().canonicalize()
            .map_err(|_| StoreError::VaultNotFound(vault_path.as_ref().to_path_buf()))?;

        if !vault_path.is_dir() {
            return Err(StoreError::VaultNotDirectory(vault_path));
        }

        Ok(Self { vault_path })
    }

    /// Get the path to a run document.
    fn run_path(&self, issue_id: &str, run_id: &str) -> PathBuf {
        self.vault_path.join("runs").join(issue_id).join(format!("{}.md", run_id))
    }

    /// Get the path to the runs directory for an issue.
    fn runs_dir(&self, issue_id: &str) -> PathBuf {
        self.vault_path.join("runs").join(issue_id)
    }

    /// Parse frontmatter from content. Returns (frontmatter, body_start_line).
    fn parse_frontmatter(content: &str) -> Option<(Frontmatter, usize)> {
        let lines: Vec<&str> = content.lines().collect();
        
        if lines.is_empty() || lines[0].trim() != "---" {
            return None;
        }

        // Find closing ---
        let mut end_idx = None;
        for (i, line) in lines.iter().enumerate().skip(1) {
            if line.trim() == "---" {
                end_idx = Some(i);
                break;
            }
        }

        let end_idx = end_idx?;
        let yaml_content = lines[1..end_idx].join("\n");
        
        let frontmatter: Frontmatter = serde_yaml::from_str(&yaml_content).ok()?;
        Some((frontmatter, end_idx + 1))
    }

    /// Scan vault root for issue files (*.md with type: issue).
    fn scan_issues(&self) -> Result<Vec<Issue>, StoreError> {
        let mut issues = Vec::new();
        let runs_dir = self.vault_path.join("runs");

        // Read only *.md files in vault root
        let entries = fs::read_dir(&self.vault_path)?;
        
        for entry in entries.filter_map(|e| e.ok()) {
            let path = entry.path();
            
            // Skip directories, runs dir, and non-markdown files
            if path.is_dir() {
                continue;
            }
            if path.extension().map_or(true, |ext| ext != "md") {
                continue;
            }
            // Skip files in runs directory (shouldn't happen at root, but be safe)
            if path.starts_with(&runs_dir) {
                continue;
            }

            // Try to parse as issue
            if let Some(issue) = self.parse_issue_file(&path)? {
                issues.insert(0, issue);
            }
        }

        Ok(issues)
    }

    /// Parse a file and return an Issue if it has type: issue frontmatter.
    fn parse_issue_file(&self, path: &Path) -> Result<Option<Issue>, StoreError> {
        let content = fs::read_to_string(path)?;
        
        let Some((frontmatter, body_start)) = Self::parse_frontmatter(&content) else {
            return Ok(None);
        };

        // Check if this is an issue file
        if frontmatter.doc_type.as_deref() != Some("issue") {
            return Ok(None);
        }

        let lines: Vec<&str> = content.lines().collect();

        // Get issue ID from frontmatter or filename
        let issue_id = frontmatter.id.unwrap_or_else(|| {
            path.file_stem()
                .map(|s| s.to_string_lossy().to_string())
                .unwrap_or_default()
        });

        // Get title from frontmatter or first H1
        let mut title = frontmatter.title.unwrap_or_default();
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
        let topic = frontmatter.topic.unwrap_or_default();
        let mut summary = frontmatter.summary.unwrap_or_default();
        if summary.is_empty() && !title.is_empty() {
            summary = if title.len() > 50 {
                format!("{}...", &title[..47])
            } else {
                title.clone()
            };
        }

        // Get status
        let status = frontmatter.status
            .map(|s| s.parse().unwrap_or_default())
            .unwrap_or_default();

        // Convert extra fields to HashMap<String, String>
        let mut fm_map = HashMap::new();
        fm_map.insert("type".to_string(), "issue".to_string());
        fm_map.insert("id".to_string(), issue_id.clone());
        for (k, v) in frontmatter.extra {
            if let Some(s) = v.as_str() {
                fm_map.insert(k, s.to_string());
            } else {
                fm_map.insert(k, format!("{:?}", v));
            }
        }

        Ok(Some(Issue {
            id: issue_id,
            title,
            topic,
            summary,
            status,
            body,
            path: path.to_path_buf(),
            frontmatter: fm_map,
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

        let lines: Vec<&str> = content.lines().collect();
        let mut body_start = 0;

        // Parse frontmatter using serde_yaml
        if let Some((frontmatter, start)) = Self::parse_frontmatter(&content) {
            body_start = start;
            run.agent = frontmatter.agent.unwrap_or_default();
            run.model = frontmatter.model.unwrap_or_default();
            run.model_variant = frontmatter.model_variant.unwrap_or_default();
            run.continued_from = frontmatter.continued_from.unwrap_or_default();
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

    /// Iterate over all run files and compute short IDs without loading full content.
    /// Returns Vec<(issue_id, run_id, short_id, path)>
    fn scan_run_short_ids(&self) -> Result<Vec<(String, String, String, PathBuf)>, StoreError> {
        let mut results = Vec::new();
        let runs_root = self.vault_path.join("runs");

        let issue_dirs = match fs::read_dir(&runs_root) {
            Ok(entries) => entries,
            Err(_) => return Ok(results),
        };

        for issue_entry in issue_dirs.filter_map(|e| e.ok()) {
            let issue_path = issue_entry.path();
            if !issue_path.is_dir() {
                continue;
            }
            
            let issue_id = match issue_entry.file_name().to_str() {
                Some(s) => s.to_string(),
                None => continue,
            };

            let run_entries = match fs::read_dir(&issue_path) {
                Ok(e) => e,
                Err(_) => continue,
            };

            for run_entry in run_entries.filter_map(|e| e.ok()) {
                let run_path = run_entry.path();
                if run_path.is_dir() || run_path.extension().map_or(true, |ext| ext != "md") {
                    continue;
                }

                let run_id = match run_path.file_stem().and_then(|s| s.to_str()) {
                    Some(s) => s.to_string(),
                    None => continue,
                };

                let short_id = generate_short_id(&issue_id, &run_id);
                results.push((issue_id.clone(), run_id, short_id, run_path));
            }
        }

        Ok(results)
    }
}

impl Store for FileStore {
    fn vault_path(&self) -> &Path {
        &self.vault_path
    }

    fn resolve_issue(&self, issue_id: &str) -> Result<Issue, StoreError> {
        // First try direct file lookup
        let direct_path = self.vault_path.join(format!("{}.md", issue_id));
        if direct_path.exists() {
            if let Some(issue) = self.parse_issue_file(&direct_path)? {
                return Ok(issue);
            }
        }

        // Fall back to scanning
        let issues = self.scan_issues()?;
        issues
            .into_iter()
            .find(|i| i.id == issue_id)
            .ok_or_else(|| StoreError::IssueNotFound(issue_id.to_string()))
    }

    fn list_issues(&self) -> Result<Vec<Issue>, StoreError> {
        self.scan_issues()
    }

    fn set_issue_status(&self, issue_id: &str, status: IssueStatus) -> Result<(), StoreError> {
        let issue = self.resolve_issue(issue_id)?;
        let content = fs::read_to_string(&issue.path)?;

        let lines: Vec<&str> = content.lines().collect();
        if lines.is_empty() || lines[0].trim() != "---" {
            return Err(StoreError::Parse {
                path: issue.path.clone(),
                message: "issue file has no frontmatter".to_string(),
            });
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
        // Optimized: compute short IDs from filenames without loading run content
        let all_runs = self.scan_run_short_ids()?;
        
        let matches: Vec<_> = all_runs
            .into_iter()
            .filter(|(_, _, sid, _)| sid.starts_with(short_id))
            .collect();

        match matches.len() {
            0 => Err(StoreError::RunNotFound(short_id.to_string())),
            1 => {
                let (issue_id, run_id, _, path) = matches.into_iter().next().unwrap();
                self.load_run(&issue_id, &run_id, &path)
            }
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
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;

    fn setup_test_vault() -> (TempDir, FileStore) {
        let temp_dir = TempDir::new().unwrap();
        let vault_path = temp_dir.path();

        // Create a test issue at vault root (not in issues/ subdirectory)
        let issue_content = r#"---
type: issue
id: test-issue
title: Test Issue
status: open
topic: testing
custom_field: custom_value
---

# Test Issue

This is a test issue body.
"#;
        fs::write(vault_path.join("test-issue.md"), issue_content).unwrap();

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
        assert_eq!(issue.topic, "testing");
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

    #[test]
    fn test_issues_not_in_runs_dir() {
        let temp_dir = TempDir::new().unwrap();
        let vault_path = temp_dir.path();

        // Create a valid issue
        let issue_content = "---\ntype: issue\nid: real-issue\n---\n# Real";
        fs::write(vault_path.join("real-issue.md"), issue_content).unwrap();

        // Create runs directory with a markdown file that looks like an issue
        let runs_dir = vault_path.join("runs").join("fake-issue");
        fs::create_dir_all(&runs_dir).unwrap();
        let fake_issue = "---\ntype: issue\nid: fake-issue\n---\n# Fake";
        fs::write(runs_dir.join("run.md"), fake_issue).unwrap();

        let store = FileStore::new(vault_path).unwrap();
        let issues = store.list_issues().unwrap();

        // Should only find the real issue, not the fake one in runs/
        assert_eq!(issues.len(), 1);
        assert_eq!(issues[0].id, "real-issue");
    }

    #[test]
    fn test_frontmatter_with_colons_in_value() {
        let temp_dir = TempDir::new().unwrap();
        let vault_path = temp_dir.path();

        // Create issue with colons in title
        let issue_content = r#"---
type: issue
id: colon-test
title: "Fix: something is broken"
---

# Test
"#;
        fs::write(vault_path.join("colon-test.md"), issue_content).unwrap();
        fs::create_dir_all(vault_path.join("runs")).unwrap();

        let store = FileStore::new(vault_path).unwrap();
        let issue = store.resolve_issue("colon-test").unwrap();
        
        assert_eq!(issue.title, "Fix: something is broken");
    }

    #[test]
    fn test_get_run_by_short_id() {
        let (_temp, store) = setup_test_vault();
        
        store.create_run("test-issue", "20240115-100000", HashMap::new()).unwrap();
        
        // Get the short ID
        let short_id = generate_short_id("test-issue", "20240115-100000");
        
        // Should find by full short ID
        let run = store.get_run_by_short_id(&short_id).unwrap();
        assert_eq!(run.run_id, "20240115-100000");
        
        // Should find by prefix (first 2 chars)
        let run = store.get_run_by_short_id(&short_id[..2]).unwrap();
        assert_eq!(run.run_id, "20240115-100000");
    }

    #[test]
    fn test_ambiguous_short_id() {
        let (_temp, store) = setup_test_vault();
        
        // Create multiple runs
        store.create_run("test-issue", "20240115-100000", HashMap::new()).unwrap();
        store.create_run("test-issue", "20240115-100001", HashMap::new()).unwrap();
        store.create_run("test-issue", "20240115-100002", HashMap::new()).unwrap();
        
        // An empty prefix should be ambiguous
        let result = store.get_run_by_short_id("");
        assert!(matches!(result, Err(StoreError::AmbiguousRunId(_, _))));
    }

    #[test]
    fn test_set_issue_status_preserves_frontmatter() {
        let (_temp, store) = setup_test_vault();
        
        // Set status to resolved
        store.set_issue_status("test-issue", IssueStatus::Resolved).unwrap();
        
        // Re-read and verify
        let issue = store.resolve_issue("test-issue").unwrap();
        assert_eq!(issue.status, IssueStatus::Resolved);
        assert_eq!(issue.title, "Test Issue"); // Title should be preserved
        assert_eq!(issue.topic, "testing"); // Topic should be preserved
    }
}
