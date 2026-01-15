//! orch CLI - Orchestrator for LLM CLIs
//!
//! A Rust implementation of the orch command-line tool.

use clap::Parser;
use orch_core::cli::{self, Cli, Commands, EXIT_INTERNAL_ERROR, EXIT_OK};

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

    // Get vault path
    let vault_path = match cli::get_vault_path(&cli) {
        Ok(path) => path,
        Err(e) => {
            eprintln!("Error: {}", e);
            std::process::exit(EXIT_INTERNAL_ERROR);
        }
    };

    // Execute command
    let result = match cli.command {
        Commands::Issue(cmd) => cmd.execute(&vault_path, cli.json, cli.tsv),
        Commands::Ps(cmd) => cmd.execute(&vault_path, cli.json, cli.tsv),
        Commands::Show(cmd) => cmd.execute(&vault_path, cli.json, cli.tsv),

        Commands::Run { issue_id, agent, model, verbose } => {
            // TODO: Implement run command
            eprintln!("Run command not yet implemented in Rust version");
            eprintln!("Issue: {}, Agent: {:?}, Model: {:?}, Verbose: {}", 
                     issue_id, agent, model, verbose);
            Ok(())
        }

        Commands::Continue { issue_ref, branch } => {
            eprintln!("Continue command not yet implemented in Rust version");
            eprintln!("Issue ref: {}, Branch: {:?}", issue_ref, branch);
            Ok(())
        }

        Commands::Attach { run_ref } => {
            // Simple attach using tmux
            match orch_core::store::FileStore::new(&vault_path) {
                Ok(store) => {
                    use orch_core::store::Store;
                    use orch_core::models::RunRef;

                    // Try to find the run
                    let run = if run_ref.len() <= 6 && run_ref.chars().all(|c| c.is_ascii_hexdigit()) {
                        store.get_run_by_short_id(&run_ref)
                    } else {
                        RunRef::parse(&run_ref)
                            .map_err(|e| orch_core::store::StoreError::Parse(e))
                            .and_then(|r| store.get_run(&r))
                    };

                    match run {
                        Ok(run) if !run.tmux_session.is_empty() => {
                            let status = std::process::Command::new("tmux")
                                .args(["attach-session", "-t", &run.tmux_session])
                                .status();
                            match status {
                                Ok(s) if s.success() => Ok(()),
                                Ok(_) => Err("Failed to attach to tmux session".to_string()),
                                Err(e) => Err(format!("Failed to run tmux: {}", e)),
                            }
                        }
                        Ok(_) => Err("Run has no tmux session".to_string()),
                        Err(e) => Err(e.to_string()),
                    }
                }
                Err(e) => Err(e.to_string()),
            }
        }

        Commands::Stop { run_ref, all } => {
            eprintln!("Stop command not yet implemented in Rust version");
            eprintln!("Run ref: {:?}, All: {}", run_ref, all);
            Ok(())
        }

        Commands::Resolve { issue_id } => {
            use orch_core::store::{FileStore, Store};
            use orch_core::models::IssueStatus;

            FileStore::new(&vault_path)
                .map_err(|e| e.to_string())
                .and_then(|store| {
                    store.set_issue_status(&issue_id, IssueStatus::Resolved)
                        .map_err(|e| e.to_string())
                })
                .map(|_| {
                    println!("Issue '{}' marked as resolved", issue_id);
                })
        }

        Commands::Open { issue_id } => {
            use orch_core::store::{FileStore, Store};

            FileStore::new(&vault_path)
                .map_err(|e| e.to_string())
                .and_then(|store| {
                    store.resolve_issue(&issue_id)
                        .map_err(|e| e.to_string())
                })
                .and_then(|issue| {
                    let editor = std::env::var("EDITOR").unwrap_or_else(|_| "vim".to_string());
                    let status = std::process::Command::new(&editor)
                        .arg(&issue.path)
                        .status();
                    match status {
                        Ok(s) if s.success() => Ok(()),
                        Ok(_) => Err("Editor exited with error".to_string()),
                        Err(e) => Err(format!("Failed to run editor: {}", e)),
                    }
                })
        }

        Commands::Monitor => {
            // Launch the Python TUI
            let status = std::process::Command::new("orch-monitor")
                .args(["--vault", vault_path.to_str().unwrap_or_default()])
                .status();
            match status {
                Ok(s) if s.success() => Ok(()),
                Ok(_) => Err("Monitor exited with error".to_string()),
                Err(e) => Err(format!("Failed to run monitor: {}", e)),
            }
        }

        Commands::Daemon { stop, restart } => {
            use orch_core::daemon;

            if stop {
                match daemon::kill(&vault_path) {
                    Ok(_) => {
                        println!("Daemon stopped");
                        Ok(())
                    }
                    Err(e) => Err(e.to_string()),
                }
            } else if restart {
                match daemon::kill(&vault_path) {
                    Ok(_) => {
                        println!("Daemon restarted (note: full daemon implementation pending)");
                        Ok(())
                    }
                    Err(e) => Err(e.to_string()),
                }
            } else {
                if daemon::is_running(&vault_path) {
                    if let Some(pid) = daemon::get_running_pid(&vault_path) {
                        println!("Daemon is running (PID: {})", pid);
                    } else {
                        println!("Daemon is running");
                    }
                } else {
                    println!("Daemon is not running");
                }
                Ok(())
            }
        }

        Commands::Repair => {
            // Basic repair: clean up stale PID files, etc.
            use orch_core::daemon;

            if !daemon::is_running(&vault_path) {
                let _ = daemon::remove_pid(&vault_path);
                println!("Cleaned up stale daemon PID file");
            }

            // TODO: Add more repair functionality
            println!("Repair completed");
            Ok(())
        }

        Commands::Delete { run_ref, force } => {
            eprintln!("Delete command not yet implemented in Rust version");
            eprintln!("Run ref: {}, Force: {}", run_ref, force);
            Ok(())
        }

        Commands::Exec { run_ref, command } => {
            eprintln!("Exec command not yet implemented in Rust version");
            eprintln!("Run ref: {}, Command: {:?}", run_ref, command);
            Ok(())
        }

        Commands::Send { run_ref, input } => {
            eprintln!("Send command not yet implemented in Rust version");
            eprintln!("Run ref: {}, Input: {}", run_ref, input);
            Ok(())
        }

        Commands::Capture { run_ref, output } => {
            eprintln!("Capture command not yet implemented in Rust version");
            eprintln!("Run ref: {}, Output: {:?}", run_ref, output);
            Ok(())
        }

        Commands::CaptureAll { output } => {
            eprintln!("CaptureAll command not yet implemented in Rust version");
            eprintln!("Output: {:?}", output);
            Ok(())
        }

        Commands::Models => {
            // List available agent models
            println!("Available agents:");
            println!("  claude    - Anthropic Claude CLI");
            println!("  codex     - OpenAI Codex CLI");
            println!("  gemini    - Google Gemini CLI");
            println!("  opencode  - OpenCode CLI");
            Ok(())
        }
    };

    match result {
        Ok(()) => std::process::exit(EXIT_OK),
        Err(e) => {
            eprintln!("Error: {}", e);
            std::process::exit(EXIT_INTERNAL_ERROR);
        }
    }
}
