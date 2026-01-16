//! Issue model representing a specification unit.

#[cfg(feature = "python")]
use pyo3::prelude::*;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fmt;
use std::path::PathBuf;
use std::str::FromStr;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, Default)]
#[cfg_attr(feature = "python", pyclass(eq, eq_int))]
#[serde(rename_all = "snake_case")]
pub enum IssueStatus {
    #[default]
    Open,
    Resolved,
    Closed,
}

impl fmt::Display for IssueStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let s = match self {
            IssueStatus::Open => "open",
            IssueStatus::Resolved => "resolved",
            IssueStatus::Closed => "closed",
        };
        write!(f, "{}", s)
    }
}

impl FromStr for IssueStatus {
    type Err = ();

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "open" => Ok(IssueStatus::Open),
            "resolved" => Ok(IssueStatus::Resolved),
            "closed" => Ok(IssueStatus::Closed),
            _ => Ok(IssueStatus::Open), // Default to open for backwards compatibility
        }
    }
}

impl IssueStatus {
    /// Get the string value.
    pub fn value(&self) -> &'static str {
        match self {
            IssueStatus::Open => "open",
            IssueStatus::Resolved => "resolved",
            IssueStatus::Closed => "closed",
        }
    }
}

#[cfg(feature = "python")]
#[pymethods]
impl IssueStatus {
    #[getter]
    fn py_value(&self) -> &'static str {
        self.value()
    }

    fn __str__(&self) -> &'static str {
        self.value()
    }

    fn __repr__(&self) -> String {
        format!("IssueStatus.{}", self.value().to_uppercase())
    }
}

/// An issue specification.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[cfg_attr(feature = "python", pyclass)]
pub struct Issue {
    pub id: String,
    pub title: String,
    pub topic: String,
    pub summary: String,
    pub status: IssueStatus,
    pub body: String,
    pub path: PathBuf,
    pub frontmatter: HashMap<String, String>,
}

impl Issue {
    /// Create a new issue with minimal required fields.
    pub fn new(id: impl Into<String>) -> Self {
        Self {
            id: id.into(),
            status: IssueStatus::Open,
            ..Default::default()
        }
    }

    /// Check if this is a valid issue status string.
    pub fn is_valid_status(s: &str) -> bool {
        matches!(s, "open" | "resolved" | "closed")
    }
}

#[cfg(feature = "python")]
#[pymethods]
impl Issue {
    #[new]
    #[pyo3(signature = (id, title="".to_string(), topic="".to_string(), summary="".to_string(), status=IssueStatus::Open, body="".to_string(), frontmatter=None))]
    fn py_new(
        id: String,
        title: String,
        topic: String,
        summary: String,
        status: IssueStatus,
        body: String,
        frontmatter: Option<HashMap<String, String>>,
    ) -> Self {
        Self {
            id,
            title,
            topic,
            summary,
            status,
            body,
            path: PathBuf::new(),
            frontmatter: frontmatter.unwrap_or_default(),
        }
    }

    #[getter]
    fn id(&self) -> String {
        self.id.clone()
    }

    #[setter]
    fn set_id(&mut self, id: String) {
        self.id = id;
    }

    #[getter]
    fn title(&self) -> String {
        self.title.clone()
    }

    #[setter]
    fn set_title(&mut self, title: String) {
        self.title = title;
    }

    #[getter]
    fn topic(&self) -> String {
        self.topic.clone()
    }

    #[setter]
    fn set_topic(&mut self, topic: String) {
        self.topic = topic;
    }

    #[getter]
    fn summary(&self) -> String {
        self.summary.clone()
    }

    #[setter]
    fn set_summary(&mut self, summary: String) {
        self.summary = summary;
    }

    #[getter]
    fn status(&self) -> IssueStatus {
        self.status
    }

    #[setter]
    fn set_status(&mut self, status: IssueStatus) {
        self.status = status;
    }

    #[getter]
    fn body(&self) -> String {
        self.body.clone()
    }

    #[setter]
    fn set_body(&mut self, body: String) {
        self.body = body;
    }

    #[getter]
    fn path(&self) -> String {
        self.path.to_string_lossy().to_string()
    }

    #[setter]
    fn set_path(&mut self, path: String) {
        self.path = PathBuf::from(path);
    }

    #[getter]
    fn frontmatter(&self) -> HashMap<String, String> {
        self.frontmatter.clone()
    }

    #[setter]
    fn set_frontmatter(&mut self, frontmatter: HashMap<String, String>) {
        self.frontmatter = frontmatter;
    }

    fn status_display(&self) -> String {
        self.status.to_string()
    }

    fn __str__(&self) -> String {
        format!("Issue({})", self.id)
    }

    fn __repr__(&self) -> String {
        format!(
            "Issue(id='{}', title='{}', status={:?})",
            self.id, self.title, self.status
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_issue_status_from_str() {
        assert_eq!(IssueStatus::from_str("open").unwrap(), IssueStatus::Open);
        assert_eq!(IssueStatus::from_str("resolved").unwrap(), IssueStatus::Resolved);
        assert_eq!(IssueStatus::from_str("closed").unwrap(), IssueStatus::Closed);
        // Unknown status defaults to open
        assert_eq!(IssueStatus::from_str("unknown").unwrap(), IssueStatus::Open);
    }

    #[test]
    fn test_issue_new() {
        let issue = Issue::new("my-issue");
        assert_eq!(issue.id, "my-issue");
        assert_eq!(issue.status, IssueStatus::Open);
    }
}
