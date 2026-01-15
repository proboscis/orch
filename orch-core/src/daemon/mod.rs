//! Daemon implementation for orch.
//!
//! The daemon monitors running agents and updates run statuses automatically.
//! This module will be fully implemented in Phase 3.

use nix::libc;
use std::path::{Path, PathBuf};
use std::time::Duration;
use thiserror::Error;

pub const DEFAULT_INTERVAL: Duration = Duration::from_secs(5);
pub const STALL_THRESHOLD: Duration = Duration::from_secs(60);
pub const FETCH_INTERVAL: Duration = Duration::from_secs(90);

#[derive(Error, Debug)]
pub enum DaemonError {
    #[error("daemon already running")]
    AlreadyRunning,

    #[error("daemon not running")]
    NotRunning,

    #[error("failed to start daemon: {0}")]
    StartFailed(String),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
}

/// Get the path to the .orch directory in the vault.
pub fn orch_dir(vault_path: impl AsRef<Path>) -> PathBuf {
    vault_path.as_ref().join(".orch")
}

/// Get the path to the PID file.
pub fn pid_file_path(vault_path: impl AsRef<Path>) -> PathBuf {
    orch_dir(vault_path).join("daemon.pid")
}

/// Get the path to the log file.
pub fn log_file_path(vault_path: impl AsRef<Path>) -> PathBuf {
    orch_dir(vault_path).join("daemon.log")
}

/// Get the path to the socket file.
pub fn socket_file_path(vault_path: impl AsRef<Path>) -> PathBuf {
    orch_dir(vault_path).join("daemon.sock")
}

/// Ensure the .orch directory exists.
pub fn ensure_orch_dir(vault_path: impl AsRef<Path>) -> std::io::Result<()> {
    std::fs::create_dir_all(orch_dir(vault_path))
}

/// Check if the daemon is running for the given vault.
pub fn is_running(vault_path: impl AsRef<Path>) -> bool {
    let pid_file = pid_file_path(&vault_path);
    if !pid_file.exists() {
        return false;
    }

    match std::fs::read_to_string(&pid_file) {
        Ok(pid_str) => {
            if let Ok(pid) = pid_str.trim().parse::<i32>() {
                // Check if process is alive using kill(pid, 0)
                unsafe {
                    libc::kill(pid, 0) == 0
                }
            } else {
                false
            }
        }
        Err(_) => false,
    }
}

/// Get the PID of the running daemon.
pub fn get_running_pid(vault_path: impl AsRef<Path>) -> Option<i32> {
    let pid_file = pid_file_path(&vault_path);
    std::fs::read_to_string(&pid_file)
        .ok()
        .and_then(|s| s.trim().parse().ok())
}

/// Write the current process PID to the PID file.
pub fn write_pid(vault_path: impl AsRef<Path>) -> std::io::Result<()> {
    let pid_file = pid_file_path(&vault_path);
    std::fs::write(&pid_file, std::process::id().to_string())
}

/// Remove the PID file.
pub fn remove_pid(vault_path: impl AsRef<Path>) -> std::io::Result<()> {
    let pid_file = pid_file_path(&vault_path);
    if pid_file.exists() {
        std::fs::remove_file(&pid_file)?;
    }
    Ok(())
}

/// Kill the daemon for the given vault.
pub fn kill(vault_path: impl AsRef<Path>) -> Result<(), DaemonError> {
    if let Some(pid) = get_running_pid(&vault_path) {
        unsafe {
            libc::kill(pid, libc::SIGTERM);
        }
        // Wait a bit for graceful shutdown
        std::thread::sleep(Duration::from_millis(500));
        // Clean up PID file
        let _ = remove_pid(&vault_path);
    }
    Ok(())
}
