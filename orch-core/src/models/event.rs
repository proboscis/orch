//! Event types and parsing for run events.

use chrono::{DateTime, Utc};
#[cfg(feature = "python")]
use pyo3::prelude::*;
use regex::Regex;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fmt;
use std::str::FromStr;
use std::sync::LazyLock;

use super::status::{Phase, Status};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[cfg_attr(feature = "python", pyclass(eq, eq_int))]
#[serde(rename_all = "snake_case")]
pub enum EventType {
    Status,
    Phase,
    Artifact,
    Test,
    Note,
}

impl fmt::Display for EventType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let s = match self {
            EventType::Status => "status",
            EventType::Phase => "phase",
            EventType::Artifact => "artifact",
            EventType::Test => "test",
            EventType::Note => "note",
        };
        write!(f, "{}", s)
    }
}

impl FromStr for EventType {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "status" => Ok(EventType::Status),
            "phase" => Ok(EventType::Phase),
            "artifact" => Ok(EventType::Artifact),
            "test" => Ok(EventType::Test),
            "note" => Ok(EventType::Note),
            _ => Err(format!("unknown event type: {}", s)),
        }
    }
}

impl EventType {
    /// Get the string value.
    pub fn value(&self) -> &'static str {
        match self {
            EventType::Status => "status",
            EventType::Phase => "phase",
            EventType::Artifact => "artifact",
            EventType::Test => "test",
            EventType::Note => "note",
        }
    }
}

#[cfg(feature = "python")]
#[pymethods]
impl EventType {
    #[getter]
    fn py_value(&self) -> &'static str {
        self.value()
    }

    fn __str__(&self) -> &'static str {
        self.value()
    }

    fn __repr__(&self) -> String {
        format!("EventType.{}", self.value().to_uppercase())
    }
}

/// A single event in a run.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[cfg_attr(feature = "python", pyclass)]
pub struct Event {
    pub timestamp: DateTime<Utc>,
    pub event_type: EventType,
    pub name: String,
    pub attrs: HashMap<String, String>,
    pub raw: String,
}

// Regex patterns for parsing event lines
// Format: - <ts> | <type> | <name> | key=value | key=value ...
static EVENT_LINE_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r#"^-\s+(\S+)\s+\|\s+(\w+)\s+\|\s+(\S+)(.*)$"#).unwrap()
});

static ATTR_REGEX: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r#"(\w+)=(?:"([^"]*)"|(\S+))"#).unwrap()
});

impl Event {
    /// Create a new event with the current timestamp.
    pub fn new(event_type: EventType, name: impl Into<String>, attrs: HashMap<String, String>) -> Self {
        Self {
            timestamp: Utc::now(),
            event_type,
            name: name.into(),
            attrs,
            raw: String::new(),
        }
    }

    /// Create a new status event.
    pub fn status(status: Status) -> Self {
        Self::new(EventType::Status, status.to_string(), HashMap::new())
    }

    /// Create a new phase event.
    pub fn phase(phase: Phase) -> Self {
        Self::new(EventType::Phase, phase.to_string(), HashMap::new())
    }

    /// Create a new artifact event.
    pub fn artifact(name: impl Into<String>, attrs: HashMap<String, String>) -> Self {
        Self::new(EventType::Artifact, name, attrs)
    }

    /// Create an error artifact event.
    pub fn error(message: impl Into<String>) -> Self {
        let mut attrs = HashMap::new();
        attrs.insert("message".to_string(), message.into());
        Self::new(EventType::Artifact, "error", attrs)
    }

    /// Parse an event line from markdown.
    pub fn parse(line: &str) -> Option<Self> {
        let line = line.trim();
        if !line.starts_with("- ") {
            return None;
        }

        let caps = EVENT_LINE_REGEX.captures(line)?;
        
        let timestamp_str = caps.get(1)?.as_str();
        let timestamp = DateTime::parse_from_rfc3339(timestamp_str)
            .ok()?
            .with_timezone(&Utc);
        
        let event_type_str = caps.get(2)?.as_str();
        let event_type = EventType::from_str(event_type_str).ok()?;
        
        let name = caps.get(3)?.as_str().to_string();
        
        let mut attrs = HashMap::new();
        if let Some(rest) = caps.get(4) {
            for attr_caps in ATTR_REGEX.captures_iter(rest.as_str()) {
                let key = attr_caps.get(1).map(|m| m.as_str().to_string());
                // Try quoted value first, then unquoted
                let value = attr_caps
                    .get(2)
                    .or_else(|| attr_caps.get(3))
                    .map(|m| m.as_str().to_string());
                
                if let (Some(k), Some(v)) = (key, value) {
                    attrs.insert(k, v);
                }
            }
        }

        Some(Self {
            timestamp,
            event_type,
            name,
            attrs,
            raw: line.to_string(),
        })
    }

    /// Format the event as a markdown line.
    pub fn to_line(&self) -> String {
        let mut parts = vec![
            format!("- {}", self.timestamp.format("%Y-%m-%dT%H:%M:%S%.3fZ")),
            self.event_type.to_string(),
            self.name.clone(),
        ];

        // Sort keys for consistent output
        let mut keys: Vec<_> = self.attrs.keys().collect();
        keys.sort();

        for key in keys {
            if let Some(value) = self.attrs.get(key) {
                let formatted_value = if value.contains(' ') || value.contains('\t') || value.contains('|') || value.contains('=') {
                    format!("\"{}\"", value)
                } else {
                    value.clone()
                };
                parts.push(format!("{}={}", key, formatted_value));
            }
        }

        parts.join(" | ")
    }

    /// Get the timestamp as a string (for Python compatibility).
    pub fn timestamp_str(&self) -> String {
        self.timestamp.format("%Y-%m-%dT%H:%M:%S%.3fZ").to_string()
    }
}

impl fmt::Display for Event {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.to_line())
    }
}

#[cfg(feature = "python")]
#[pymethods]
impl Event {
    #[new]
    #[pyo3(signature = (event_type, name, attrs=None))]
    fn py_new(event_type: EventType, name: String, attrs: Option<HashMap<String, String>>) -> Self {
        Self::new(event_type, name, attrs.unwrap_or_default())
    }

    #[getter]
    fn event_type(&self) -> EventType {
        self.event_type
    }

    #[getter]
    fn name(&self) -> String {
        self.name.clone()
    }

    #[getter]
    fn attrs(&self) -> HashMap<String, String> {
        self.attrs.clone()
    }

    #[getter]
    fn raw(&self) -> String {
        self.raw.clone()
    }

    #[getter]
    fn timestamp(&self) -> String {
        self.timestamp_str()
    }

    #[staticmethod]
    #[pyo3(name = "parse")]
    fn py_parse(line: &str) -> Option<Self> {
        Self::parse(line)
    }

    fn __str__(&self) -> String {
        self.to_line()
    }

    fn __repr__(&self) -> String {
        format!(
            "Event(type={:?}, name='{}', timestamp={})",
            self.event_type, self.name, self.timestamp_str()
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_simple_event() {
        let line = "- 2024-01-15T10:30:00Z | status | running";
        let event = Event::parse(line).unwrap();
        assert_eq!(event.event_type, EventType::Status);
        assert_eq!(event.name, "running");
        assert!(event.attrs.is_empty());
    }

    #[test]
    fn test_parse_event_with_attrs() {
        let line = "- 2024-01-15T10:30:00Z | artifact | worktree | path=/tmp/worktree";
        let event = Event::parse(line).unwrap();
        assert_eq!(event.event_type, EventType::Artifact);
        assert_eq!(event.name, "worktree");
        assert_eq!(event.attrs.get("path"), Some(&"/tmp/worktree".to_string()));
    }

    #[test]
    fn test_parse_event_with_quoted_attrs() {
        let line = "- 2024-01-15T10:30:00Z | artifact | error | message=\"something went wrong\"";
        let event = Event::parse(line).unwrap();
        assert_eq!(event.attrs.get("message"), Some(&"something went wrong".to_string()));
    }

    #[test]
    fn test_event_to_line() {
        let event = Event::status(Status::Running);
        let line = event.to_line();
        assert!(line.contains("| status | running"));
    }
}
