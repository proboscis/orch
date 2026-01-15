//! Status and phase types for runs.

#[cfg(feature = "python")]
use pyo3::prelude::*;
use serde::{Deserialize, Serialize};
use std::fmt;
use std::str::FromStr;

/// Run operational lifecycle states.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[cfg_attr(feature = "python", pyclass(eq, eq_int))]
#[serde(rename_all = "snake_case")]
pub enum Status {
    #[cfg_attr(feature = "python", pyo3(name = "QUEUED"))]
    Queued,
    #[cfg_attr(feature = "python", pyo3(name = "BOOTING"))]
    Booting,
    #[cfg_attr(feature = "python", pyo3(name = "RUNNING"))]
    Running,
    #[cfg_attr(feature = "python", pyo3(name = "BLOCKED"))]
    Blocked,
    #[cfg_attr(feature = "python", pyo3(name = "BLOCKED_API"))]
    BlockedApi,
    #[cfg_attr(feature = "python", pyo3(name = "PR_OPEN"))]
    PrOpen,
    #[cfg_attr(feature = "python", pyo3(name = "DONE"))]
    Done,
    #[cfg_attr(feature = "python", pyo3(name = "FAILED"))]
    Failed,
    #[cfg_attr(feature = "python", pyo3(name = "CANCELED"))]
    Canceled,
    #[cfg_attr(feature = "python", pyo3(name = "UNKNOWN"))]
    Unknown,
}

impl Default for Status {
    fn default() -> Self {
        Self::Queued
    }
}

impl fmt::Display for Status {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let s = match self {
            Status::Queued => "queued",
            Status::Booting => "booting",
            Status::Running => "running",
            Status::Blocked => "blocked",
            Status::BlockedApi => "blocked_api",
            Status::PrOpen => "pr_open",
            Status::Done => "done",
            Status::Failed => "failed",
            Status::Canceled => "canceled",
            Status::Unknown => "unknown",
        };
        write!(f, "{}", s)
    }
}

impl FromStr for Status {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "queued" => Ok(Status::Queued),
            "booting" => Ok(Status::Booting),
            "running" => Ok(Status::Running),
            "blocked" => Ok(Status::Blocked),
            "blocked_api" => Ok(Status::BlockedApi),
            "pr_open" => Ok(Status::PrOpen),
            "done" => Ok(Status::Done),
            "failed" => Ok(Status::Failed),
            "canceled" => Ok(Status::Canceled),
            "unknown" => Ok(Status::Unknown),
            _ => Err(format!("unknown status: {}", s)),
        }
    }
}

impl Status {
    /// Check if this is a terminal status (run is finished).
    pub fn is_terminal(&self) -> bool {
        matches!(self, Status::Done | Status::Failed | Status::Canceled)
    }

    /// Check if this is an active status (run is in progress).
    pub fn is_active(&self) -> bool {
        matches!(
            self,
            Status::Queued
                | Status::Booting
                | Status::Running
                | Status::Blocked
                | Status::BlockedApi
                | Status::PrOpen
                | Status::Unknown
        )
    }

    /// Get the string value.
    pub fn value(&self) -> &'static str {
        match self {
            Status::Queued => "queued",
            Status::Booting => "booting",
            Status::Running => "running",
            Status::Blocked => "blocked",
            Status::BlockedApi => "blocked_api",
            Status::PrOpen => "pr_open",
            Status::Done => "done",
            Status::Failed => "failed",
            Status::Canceled => "canceled",
            Status::Unknown => "unknown",
        }
    }
}

#[cfg(feature = "python")]
#[pymethods]
impl Status {
    #[getter]
    fn py_value(&self) -> &'static str {
        self.value()
    }

    fn __str__(&self) -> &'static str {
        self.value()
    }

    fn __repr__(&self) -> String {
        format!("Status.{}", self.value().to_uppercase())
    }
}

/// Work phases for tracking progress.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[cfg_attr(feature = "python", pyclass(eq, eq_int))]
#[serde(rename_all = "snake_case")]
pub enum Phase {
    #[cfg_attr(feature = "python", pyo3(name = "PLAN"))]
    Plan,
    #[cfg_attr(feature = "python", pyo3(name = "IMPLEMENT"))]
    Implement,
    #[cfg_attr(feature = "python", pyo3(name = "TEST"))]
    Test,
    #[cfg_attr(feature = "python", pyo3(name = "PR"))]
    Pr,
    #[cfg_attr(feature = "python", pyo3(name = "REVIEW"))]
    Review,
}

impl fmt::Display for Phase {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let s = match self {
            Phase::Plan => "plan",
            Phase::Implement => "implement",
            Phase::Test => "test",
            Phase::Pr => "pr",
            Phase::Review => "review",
        };
        write!(f, "{}", s)
    }
}

impl FromStr for Phase {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "plan" => Ok(Phase::Plan),
            "implement" => Ok(Phase::Implement),
            "test" => Ok(Phase::Test),
            "pr" => Ok(Phase::Pr),
            "review" => Ok(Phase::Review),
            _ => Err(format!("unknown phase: {}", s)),
        }
    }
}

impl Phase {
    /// Get the string value.
    pub fn value(&self) -> &'static str {
        match self {
            Phase::Plan => "plan",
            Phase::Implement => "implement",
            Phase::Test => "test",
            Phase::Pr => "pr",
            Phase::Review => "review",
        }
    }
}

#[cfg(feature = "python")]
#[pymethods]
impl Phase {
    #[getter]
    fn py_value(&self) -> &'static str {
        self.value()
    }

    fn __str__(&self) -> &'static str {
        self.value()
    }

    fn __repr__(&self) -> String {
        format!("Phase.{}", self.value().to_uppercase())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_status_from_str() {
        assert_eq!(Status::from_str("running").unwrap(), Status::Running);
        assert_eq!(Status::from_str("blocked_api").unwrap(), Status::BlockedApi);
        assert!(Status::from_str("invalid").is_err());
    }

    #[test]
    fn test_status_is_terminal() {
        assert!(Status::Done.is_terminal());
        assert!(Status::Failed.is_terminal());
        assert!(Status::Canceled.is_terminal());
        assert!(!Status::Running.is_terminal());
    }

    #[test]
    fn test_phase_from_str() {
        assert_eq!(Phase::from_str("plan").unwrap(), Phase::Plan);
        assert_eq!(Phase::from_str("implement").unwrap(), Phase::Implement);
        assert!(Phase::from_str("invalid").is_err());
    }
}
