use chrono::{DateTime, Utc};
use pyo3::prelude::*;
use pyo3::types::PyDict;
use regex::Regex;
use sha2::Sha256;
use sha2::Digest as Sha2Digest;
use std::collections::{HashMap, HashSet};
use std::fs;
use std::path::{Path, PathBuf};
use walkdir::WalkDir;

fn truncate_utf8(s: &str, max_chars: usize) -> String {
    s.chars().take(max_chars).collect()
}

fn truncate_utf8_with_ellipsis(s: &str, max_chars: usize) -> String {
    if s.chars().count() > max_chars {
        let truncated: String = s.chars().take(max_chars.saturating_sub(3)).collect();
        format!("{}...", truncated)
    } else {
        s.to_string()
    }
}

#[derive(Debug, Clone, PartialEq)]
pub enum IssueStatus {
    Open,
    Resolved,
    Closed,
}

impl IssueStatus {
    fn from_str(s: &str) -> Self {
        match s.to_lowercase().as_str() {
            "resolved" => IssueStatus::Resolved,
            "closed" => IssueStatus::Closed,
            _ => IssueStatus::Open,
        }
    }

    fn to_str(&self) -> &'static str {
        match self {
            IssueStatus::Open => "open",
            IssueStatus::Resolved => "resolved",
            IssueStatus::Closed => "closed",
        }
    }
}

#[pyclass]
#[derive(Debug, Clone)]
pub struct Issue {
    #[pyo3(get)]
    pub id: String,
    #[pyo3(get)]
    pub title: String,
    #[pyo3(get)]
    pub topic: String,
    #[pyo3(get)]
    pub summary: String,
    #[pyo3(get)]
    pub status: String,
    #[pyo3(get)]
    pub body: String,
    #[pyo3(get)]
    pub path: String,
    pub frontmatter: HashMap<String, String>,
}

#[pymethods]
impl Issue {
    #[getter]
    fn frontmatter(&self, py: Python<'_>) -> PyResult<PyObject> {
        let dict = PyDict::new_bound(py);
        for (k, v) in &self.frontmatter {
            dict.set_item(k, v)?;
        }
        Ok(dict.into())
    }
}

#[derive(Debug, Clone, PartialEq)]
pub enum EventType {
    Status,
    Phase,
    Artifact,
    Test,
    Note,
}

impl EventType {
    fn from_str(s: &str) -> Option<Self> {
        match s.to_lowercase().as_str() {
            "status" => Some(EventType::Status),
            "phase" => Some(EventType::Phase),
            "artifact" => Some(EventType::Artifact),
            "test" => Some(EventType::Test),
            "note" => Some(EventType::Note),
            _ => None,
        }
    }
}

#[pyclass]
#[derive(Debug, Clone)]
pub struct Event {
    #[pyo3(get)]
    pub timestamp: String,
    #[pyo3(get)]
    pub event_type: String,
    #[pyo3(get)]
    pub name: String,
    #[pyo3(get)]
    pub raw: String,
    pub attrs: HashMap<String, String>,
    pub parsed_timestamp: Option<DateTime<Utc>>,
    pub parsed_type: Option<EventType>,
}

#[pymethods]
impl Event {
    #[getter]
    fn attrs(&self, py: Python<'_>) -> PyResult<PyObject> {
        let dict = PyDict::new_bound(py);
        for (k, v) in &self.attrs {
            dict.set_item(k, v)?;
        }
        Ok(dict.into())
    }
}

#[pyclass]
#[derive(Debug, Clone)]
pub struct Run {
    #[pyo3(get)]
    pub issue_id: String,
    #[pyo3(get)]
    pub run_id: String,
    #[pyo3(get)]
    pub path: String,
    #[pyo3(get)]
    pub status: String,
    #[pyo3(get)]
    pub phase: Option<String>,
    #[pyo3(get)]
    pub started_at: Option<String>,
    #[pyo3(get)]
    pub updated_at: Option<String>,
    #[pyo3(get)]
    pub agent: String,
    #[pyo3(get)]
    pub model: String,
    #[pyo3(get)]
    pub model_variant: String,
    #[pyo3(get)]
    pub branch: String,
    #[pyo3(get)]
    pub worktree_path: String,
    #[pyo3(get)]
    pub tmux_session: String,
    #[pyo3(get)]
    pub tmux_window_id: String,
    #[pyo3(get)]
    pub pr_url: String,
    #[pyo3(get)]
    pub server_port: i32,
    #[pyo3(get)]
    pub opencode_session_id: String,
    #[pyo3(get)]
    pub continued_from: String,
    pub events: Vec<Event>,
}

#[pymethods]
impl Run {
    #[getter]
    fn events(&self, py: Python<'_>) -> PyResult<Vec<PyObject>> {
        self.events
            .iter()
            .map(|e| {
                let event = e.clone();
                Ok(Py::new(py, event)?.into_py(py))
            })
            .collect()
    }

    fn ref_(&self) -> String {
        format!("{}#{}", self.issue_id, self.run_id)
    }

    fn short_id(&self) -> String {
        let ref_str = format!("{}#{}", self.issue_id, self.run_id);
        let mut hasher = Sha256::new();
        Sha2Digest::update(&mut hasher, ref_str.as_bytes());
        let result = hasher.finalize();
        hex::encode(&result[..3])
    }

    fn elapsed_time(&self) -> String {
        let started = match &self.started_at {
            Some(s) => match DateTime::parse_from_rfc3339(s) {
                Ok(dt) => dt.with_timezone(&Utc),
                Err(_) => return "-".to_string(),
            },
            None => return "-".to_string(),
        };

        let ended = match &self.updated_at {
            Some(s) => match DateTime::parse_from_rfc3339(s) {
                Ok(dt) => dt.with_timezone(&Utc),
                Err(_) => Utc::now(),
            },
            None => Utc::now(),
        };

        let delta = ended.signed_duration_since(started);
        let total_seconds = delta.num_seconds();

        if total_seconds < 0 {
            return "-".to_string();
        }

        let hours = total_seconds / 3600;
        let minutes = (total_seconds % 3600) / 60;
        let seconds = total_seconds % 60;

        if hours > 0 {
            format!("{}h{}m", hours, minutes)
        } else if minutes > 0 {
            format!("{}m{}s", minutes, seconds)
        } else {
            format!("{}s", seconds)
        }
    }
}

#[pyclass]
pub struct VaultReader {
    vault_path: PathBuf,
}

#[pymethods]
impl VaultReader {
    #[new]
    fn new(vault_path: String) -> Self {
        VaultReader {
            vault_path: PathBuf::from(vault_path),
        }
    }

    #[pyo3(signature = (include_resolved=false, include_closed=true))]
    fn list_issues(&self, include_resolved: bool, include_closed: bool) -> PyResult<Vec<Issue>> {
        let issues = self.scan_issues()?;
        let filtered: Vec<Issue> = issues
            .into_iter()
            .filter(|issue| {
                let status = IssueStatus::from_str(&issue.status);
                if status == IssueStatus::Resolved && !include_resolved {
                    return false;
                }
                if status == IssueStatus::Closed && !include_closed {
                    return false;
                }
                true
            })
            .collect();
        Ok(filtered)
    }

    fn get_issue(&self, issue_id: String) -> PyResult<Option<Issue>> {
        let issues = self.scan_issues()?;
        Ok(issues.into_iter().find(|i| i.id == issue_id))
    }

    #[pyo3(signature = (issue_id=None))]
    fn list_runs(&self, issue_id: Option<String>) -> PyResult<Vec<Run>> {
        let runs_root = self.vault_path.join("runs");
        if !runs_root.exists() {
            return Ok(Vec::new());
        }

        let mut runs = Vec::new();

        let issue_dirs: Vec<String> = if let Some(id) = issue_id {
            vec![id]
        } else {
            match fs::read_dir(&runs_root) {
                Ok(entries) => entries
                    .filter_map(|e| e.ok())
                    .filter(|e| e.path().is_dir())
                    .filter_map(|e| e.file_name().to_str().map(|s| s.to_string()))
                    .collect(),
                Err(_) => Vec::new(),
            }
        };

        for issue_id in issue_dirs {
            let issue_runs_dir = runs_root.join(&issue_id);
            if !issue_runs_dir.is_dir() {
                continue;
            }

            let entries = match fs::read_dir(&issue_runs_dir) {
                Ok(e) => e,
                Err(_) => continue,
            };

            for entry in entries.filter_map(|e| e.ok()) {
                let path = entry.path();
                if !path.is_file() {
                    continue;
                }
                if path.extension().and_then(|s| s.to_str()) != Some("md") {
                    continue;
                }

                let run_id = path
                    .file_stem()
                    .and_then(|s| s.to_str())
                    .map(|s| s.to_string())
                    .unwrap_or_default();

                if let Ok(run) = self.load_run(&issue_id, &run_id, &path) {
                    runs.push(run);
                }
            }
        }

        Ok(runs)
    }

    fn get_run(&self, issue_id: String, run_id: String) -> PyResult<Option<Run>> {
        let run_path = self.vault_path.join("runs").join(&issue_id).join(format!("{}.md", run_id));
        if !run_path.exists() {
            return Ok(None);
        }
        match self.load_run(&issue_id, &run_id, &run_path) {
            Ok(run) => Ok(Some(run)),
            Err(_) => Ok(None),
        }
    }

    fn get_run_content(&self, issue_id: String, run_id: String) -> PyResult<String> {
        let run_path = self.vault_path.join("runs").join(&issue_id).join(format!("{}.md", run_id));
        if !run_path.exists() {
            return Ok(String::new());
        }
        Ok(fs::read_to_string(&run_path).unwrap_or_default())
    }
}

impl VaultReader {
    fn scan_issues(&self) -> PyResult<Vec<Issue>> {
        let mut issues = Vec::new();
        let runs_dir = self.vault_path.join("runs");
        let mut visited: HashSet<PathBuf> = HashSet::new();

        for entry in WalkDir::new(&self.vault_path)
            .follow_links(true)
            .into_iter()
            .filter_map(|e| e.ok())
        {
            let path = entry.path();

            if path.starts_with(&runs_dir) {
                continue;
            }

            if let Ok(real_path) = fs::canonicalize(path) {
                if visited.contains(&real_path) {
                    continue;
                }
                visited.insert(real_path);
            }

            if !path.is_file() {
                continue;
            }
            if path.extension().and_then(|s| s.to_str()) != Some("md") {
                continue;
            }

            if let Ok(Some(issue)) = self.parse_issue_file(path) {
                issues.push(issue);
            }
        }

        Ok(issues)
    }

    fn parse_issue_file(&self, path: &Path) -> PyResult<Option<Issue>> {
        let content = match fs::read_to_string(path) {
            Ok(c) => c,
            Err(_) => return Ok(None),
        };

        let lines: Vec<&str> = content.lines().collect();
        if lines.is_empty() || lines[0].trim() != "---" {
            return Ok(None);
        }

        let mut frontmatter: HashMap<String, String> = HashMap::new();
        let mut body_start = 0;

        for (i, line) in lines.iter().enumerate().skip(1) {
            if line.trim() == "---" {
                body_start = i + 1;
                break;
            }
            if let Some(colon_pos) = line.find(':') {
                let key = line[..colon_pos].trim().to_string();
                let value = line[colon_pos + 1..].trim().to_string();
                frontmatter.insert(key, value);
            }
        }

        if frontmatter.get("type").map(|s| s.as_str()) != Some("issue") {
            return Ok(None);
        }

        let issue_id = frontmatter
            .get("id")
            .cloned()
            .unwrap_or_else(|| {
                path.file_stem()
                    .and_then(|s| s.to_str())
                    .map(|s| s.to_string())
                    .unwrap_or_default()
            });

        let mut title = frontmatter.get("title").cloned().unwrap_or_default();
        if title.is_empty() && body_start < lines.len() {
            for line in &lines[body_start..] {
                if line.starts_with("# ") {
                    title = line[2..].to_string();
                    break;
                }
            }
        }

        let body = if body_start < lines.len() {
            lines[body_start..].join("\n")
        } else {
            String::new()
        };

        let topic = frontmatter.get("topic").cloned().unwrap_or_else(|| {
            truncate_utf8(&title, 50)
        });

        let summary = frontmatter.get("summary").cloned().unwrap_or_else(|| {
            truncate_utf8_with_ellipsis(&title, 50)
        });

        let status = IssueStatus::from_str(
            frontmatter.get("status").map(|s| s.as_str()).unwrap_or("open"),
        );

        Ok(Some(Issue {
            id: issue_id,
            title,
            topic,
            summary,
            status: status.to_str().to_string(),
            body,
            path: path.to_string_lossy().to_string(),
            frontmatter,
        }))
    }

    fn load_run(&self, issue_id: &str, run_id: &str, path: &Path) -> PyResult<Run> {
        let content = fs::read_to_string(path)
            .map_err(|e| pyo3::exceptions::PyIOError::new_err(e.to_string()))?;

        let lines: Vec<&str> = content.lines().collect();
        let mut body_start = 0;
        let mut agent = String::new();
        let mut model = String::new();
        let mut model_variant = String::new();
        let mut continued_from = String::new();

        if !lines.is_empty() && lines[0].trim() == "---" {
            for (i, line) in lines.iter().enumerate().skip(1) {
                if line.trim() == "---" {
                    body_start = i + 1;
                    break;
                }
                if let Some(colon_pos) = line.find(':') {
                    let key = line[..colon_pos].trim();
                    let value = line[colon_pos + 1..].trim();
                    match key {
                        "agent" => agent = value.to_string(),
                        "model" => model = value.to_string(),
                        "model_variant" => model_variant = value.to_string(),
                        "continued_from" => continued_from = value.to_string(),
                        _ => {}
                    }
                }
            }
        }

        let event_pattern = Regex::new(r"^-\s+\d{4}-\d{2}-\d{2}").unwrap();
        let mut events = Vec::new();

        for line in &lines[body_start..] {
            if event_pattern.is_match(line) {
                if let Some(event) = parse_event(line) {
                    events.push(event);
                }
            }
        }

        let mut run = Run {
            issue_id: issue_id.to_string(),
            run_id: run_id.to_string(),
            path: path.to_string_lossy().to_string(),
            status: "queued".to_string(),
            phase: None,
            started_at: None,
            updated_at: None,
            agent,
            model,
            model_variant,
            branch: String::new(),
            worktree_path: String::new(),
            tmux_session: String::new(),
            tmux_window_id: String::new(),
            pr_url: String::new(),
            server_port: 0,
            opencode_session_id: String::new(),
            continued_from,
            events,
        };

        derive_state(&mut run);

        if !run.worktree_path.is_empty() && !Path::new(&run.worktree_path).is_absolute() {
            run.worktree_path = self.vault_path.join(&run.worktree_path).to_string_lossy().to_string();
        }

        Ok(run)
    }
}

fn parse_event(line: &str) -> Option<Event> {
    let line = line.trim();
    if !line.starts_with("- ") {
        return None;
    }

    let event_regex = Regex::new(r"^-\s+(\S+)\s+\|\s+(\w+)\s+\|\s+(\S+)(.*)$").ok()?;
    let attr_regex = Regex::new(r#"(\w+)=(?:"([^"]*)"|(\S+))"#).ok()?;

    let captures = event_regex.captures(line)?;
    let timestamp_str = captures.get(1)?.as_str();
    let type_str = captures.get(2)?.as_str();
    let name = captures.get(3)?.as_str().to_string();
    let rest = captures.get(4).map(|m| m.as_str()).unwrap_or("");

    let parsed_timestamp = DateTime::parse_from_rfc3339(timestamp_str)
        .ok()
        .map(|dt| dt.with_timezone(&Utc));
    let parsed_type = EventType::from_str(type_str);

    let mut attrs = HashMap::new();
    for cap in attr_regex.captures_iter(rest) {
        let key = cap.get(1).map(|m| m.as_str()).unwrap_or("");
        let value = cap
            .get(2)
            .or_else(|| cap.get(3))
            .map(|m| m.as_str())
            .unwrap_or("");
        attrs.insert(key.to_string(), value.to_string());
    }

    Some(Event {
        timestamp: timestamp_str.to_string(),
        event_type: type_str.to_string(),
        name,
        raw: line.to_string(),
        attrs,
        parsed_timestamp,
        parsed_type,
    })
}

fn derive_state(run: &mut Run) {
    for event in run.events.iter().rev() {
        if event.parsed_type == Some(EventType::Status) {
            run.status = event.name.clone();
            break;
        }
    }

    for event in run.events.iter().rev() {
        if event.parsed_type == Some(EventType::Phase) {
            run.phase = Some(event.name.clone());
            break;
        }
    }

    for event in &run.events {
        if event.parsed_type == Some(EventType::Artifact) {
            match event.name.as_str() {
                "worktree" => {
                    if let Some(path) = event.attrs.get("path") {
                        run.worktree_path = path.clone();
                    }
                }
                "branch" => {
                    if let Some(name) = event.attrs.get("name") {
                        run.branch = name.clone();
                    }
                }
                "session" => {
                    if let Some(name) = event.attrs.get("name") {
                        run.tmux_session = name.clone();
                    }
                }
                "window" => {
                    if let Some(id) = event.attrs.get("id") {
                        run.tmux_window_id = id.clone();
                    }
                }
                "pr" => {
                    if let Some(url) = event.attrs.get("url") {
                        run.pr_url = url.clone();
                    }
                }
                "server" => {
                    if let Some(port) = event.attrs.get("port") {
                        run.server_port = port.parse().unwrap_or(0);
                    }
                }
                "opencode_session" => {
                    if let Some(id) = event.attrs.get("id") {
                        run.opencode_session_id = id.clone();
                    }
                }
                "agent_model" => {
                    if run.model.is_empty() {
                        if let Some(m) = event.attrs.get("model") {
                            run.model = m.clone();
                        }
                    }
                    if run.model_variant.is_empty() {
                        if let Some(v) = event.attrs.get("variant") {
                            run.model_variant = v.clone();
                        }
                    }
                }
                _ => {}
            }
        }
    }

    if !run.events.is_empty() {
        if let Some(first) = run.events.first() {
            run.started_at = Some(first.timestamp.clone());
        }
        if let Some(last) = run.events.last() {
            run.updated_at = Some(last.timestamp.clone());
        }
    }
}

#[pymodule]
fn orch_vault_rs(m: &Bound<'_, PyModule>) -> PyResult<()> {
    m.add_class::<VaultReader>()?;
    m.add_class::<Issue>()?;
    m.add_class::<Run>()?;
    m.add_class::<Event>()?;
    Ok(())
}
