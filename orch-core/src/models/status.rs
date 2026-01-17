//! Status and phase types for runs.

#[cfg(feature = "python")]
use pyo3::prelude::*;
use serde::{Deserialize, Serialize};
use std::fmt;
use std::str::FromStr;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[cfg_attr(feature = "python", pyclass(eq, eq_int))]
#[serde(rename_all = "snake_case")]
pub enum Status {
    Queued,
    Booting,
    Running,
    Blocked,
    BlockedApi,
    PrOpen,
    Done,
    Failed,
    Canceled,
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

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[cfg_attr(feature = "python", pyclass(eq, eq_int))]
#[serde(rename_all = "snake_case")]
pub enum Phase {
    Plan,
    Implement,
    Test,
    Pr,
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

    #[test]
    fn test_status_json_serialization() {
        let statuses = [
            Status::Queued,
            Status::Booting,
            Status::Running,
            Status::Blocked,
            Status::BlockedApi,
            Status::PrOpen,
            Status::Done,
            Status::Failed,
            Status::Canceled,
            Status::Unknown,
        ];
        
        for status in statuses {
            let json = serde_json::to_string(&status).unwrap();
            let deserialized: Status = serde_json::from_str(&json).unwrap();
            assert_eq!(deserialized, status);
        }
    }

    #[test]
    fn test_status_yaml_serialization() {
        let statuses = [
            Status::Queued,
            Status::Booting,
            Status::Running,
            Status::Blocked,
            Status::BlockedApi,
            Status::PrOpen,
            Status::Done,
            Status::Failed,
            Status::Canceled,
            Status::Unknown,
        ];
        
        for status in statuses {
            let yaml = serde_yaml::to_string(&status).unwrap();
            let deserialized: Status = serde_yaml::from_str(&yaml).unwrap();
            assert_eq!(deserialized, status);
        }
    }

    #[test]
    fn test_phase_json_serialization() {
        let phases = [
            Phase::Plan,
            Phase::Implement,
            Phase::Test,
            Phase::Pr,
            Phase::Review,
        ];
        
        for phase in phases {
            let json = serde_json::to_string(&phase).unwrap();
            let deserialized: Phase = serde_json::from_str(&json).unwrap();
            assert_eq!(deserialized, phase);
        }
    }

    #[test]
    fn test_phase_yaml_serialization() {
        let phases = [
            Phase::Plan,
            Phase::Implement,
            Phase::Test,
            Phase::Pr,
            Phase::Review,
        ];
        
        for phase in phases {
            let yaml = serde_yaml::to_string(&phase).unwrap();
            let deserialized: Phase = serde_yaml::from_str(&yaml).unwrap();
            assert_eq!(deserialized, phase);
        }
    }

    #[test]
    fn test_status_display() {
        assert_eq!(Status::Queued.to_string(), "queued");
        assert_eq!(Status::Booting.to_string(), "booting");
        assert_eq!(Status::Running.to_string(), "running");
        assert_eq!(Status::Blocked.to_string(), "blocked");
        assert_eq!(Status::BlockedApi.to_string(), "blocked_api");
        assert_eq!(Status::PrOpen.to_string(), "pr_open");
        assert_eq!(Status::Done.to_string(), "done");
        assert_eq!(Status::Failed.to_string(), "failed");
        assert_eq!(Status::Canceled.to_string(), "canceled");
        assert_eq!(Status::Unknown.to_string(), "unknown");
    }

    #[test]
    fn test_status_value() {
        assert_eq!(Status::Queued.value(), "queued");
        assert_eq!(Status::Booting.value(), "booting");
        assert_eq!(Status::Running.value(), "running");
        assert_eq!(Status::Blocked.value(), "blocked");
        assert_eq!(Status::BlockedApi.value(), "blocked_api");
        assert_eq!(Status::PrOpen.value(), "pr_open");
        assert_eq!(Status::Done.value(), "done");
        assert_eq!(Status::Failed.value(), "failed");
        assert_eq!(Status::Canceled.value(), "canceled");
        assert_eq!(Status::Unknown.value(), "unknown");
    }

    #[test]
    fn test_status_is_active() {
        assert!(Status::Queued.is_active());
        assert!(Status::Booting.is_active());
        assert!(Status::Running.is_active());
        assert!(Status::Blocked.is_active());
        assert!(Status::BlockedApi.is_active());
        assert!(Status::PrOpen.is_active());
        assert!(Status::Unknown.is_active());
        assert!(!Status::Done.is_active());
        assert!(!Status::Failed.is_active());
        assert!(!Status::Canceled.is_active());
    }

    #[test]
    fn test_phase_display() {
        assert_eq!(Phase::Plan.to_string(), "plan");
        assert_eq!(Phase::Implement.to_string(), "implement");
        assert_eq!(Phase::Test.to_string(), "test");
        assert_eq!(Phase::Pr.to_string(), "pr");
        assert_eq!(Phase::Review.to_string(), "review");
    }

    #[test]
    fn test_phase_value() {
        assert_eq!(Phase::Plan.value(), "plan");
        assert_eq!(Phase::Implement.value(), "implement");
        assert_eq!(Phase::Test.value(), "test");
        assert_eq!(Phase::Pr.value(), "pr");
        assert_eq!(Phase::Review.value(), "review");
    }

    #[test]
    fn test_status_default() {
        let status = Status::default();
        assert_eq!(status, Status::Queued);
    }

    #[test]
    fn test_status_from_str_all_values() {
        assert_eq!(Status::from_str("queued").unwrap(), Status::Queued);
        assert_eq!(Status::from_str("booting").unwrap(), Status::Booting);
        assert_eq!(Status::from_str("running").unwrap(), Status::Running);
        assert_eq!(Status::from_str("blocked").unwrap(), Status::Blocked);
        assert_eq!(Status::from_str("blocked_api").unwrap(), Status::BlockedApi);
        assert_eq!(Status::from_str("pr_open").unwrap(), Status::PrOpen);
        assert_eq!(Status::from_str("done").unwrap(), Status::Done);
        assert_eq!(Status::from_str("failed").unwrap(), Status::Failed);
        assert_eq!(Status::from_str("canceled").unwrap(), Status::Canceled);
        assert_eq!(Status::from_str("unknown").unwrap(), Status::Unknown);
    }

    #[test]
    fn test_phase_from_str_all_values() {
        assert_eq!(Phase::from_str("plan").unwrap(), Phase::Plan);
        assert_eq!(Phase::from_str("implement").unwrap(), Phase::Implement);
        assert_eq!(Phase::from_str("test").unwrap(), Phase::Test);
        assert_eq!(Phase::from_str("pr").unwrap(), Phase::Pr);
        assert_eq!(Phase::from_str("review").unwrap(), Phase::Review);
    }
}
