"""Entry point for orch monitor TUI."""

import argparse
import os
import sys
from pathlib import Path

from .app import OrchMonitorApp


def main():
    parser = argparse.ArgumentParser(description="Orch monitor TUI")
    parser.add_argument(
        "--vault",
        type=Path,
        help="Path to orch vault directory (overrides ORCH_VAULT env var)",
    )

    args = parser.parse_args()

    vault_path = None
    if args.vault:
        vault_path = args.vault
    else:
        vault_env = os.getenv("ORCH_VAULT")
        if vault_env:
            vault_path = Path(vault_env)

    app = OrchMonitorApp(vault_path=vault_path)
    app.run()


if __name__ == "__main__":
    main()
