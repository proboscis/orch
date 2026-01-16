//! orch CLI - Orchestrator for LLM CLIs
//!
//! A Rust implementation of the orch command-line tool.

use clap::Parser;
use orch_core::cli::{self, Cli, EXIT_OK};

fn main() {
    let cli = Cli::parse();

    // Initialize logging
    let log_level = match cli.log_level.to_lowercase().as_str() {
        "error" => tracing::Level::ERROR,
        "warn" => tracing::Level::WARN,
        "info" => tracing::Level::INFO,
        "debug" => tracing::Level::DEBUG,
        _ => tracing::Level::WARN,
    };

    tracing_subscriber::fmt()
        .with_max_level(log_level)
        .with_target(false)
        .init();

    // Execute command and handle errors
    match cli::execute(cli) {
        Ok(()) => std::process::exit(EXIT_OK),
        Err(e) => {
            eprintln!("Error: {:#}", e);
            std::process::exit(cli::exit_code_for_error(&e));
        }
    }
}
