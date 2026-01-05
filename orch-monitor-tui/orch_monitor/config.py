import os
from dataclasses import dataclass
from pathlib import Path

import yaml


@dataclass
class OrchConfig:
    vault: str
    agent: str = "claude"
    base_branch: str = "main"
    pr_target_branch: str = "main"


def load_config(config_path: Path | None = None) -> OrchConfig:
    if config_path is None:
        config_path = Path.cwd() / ".orch" / "config.yaml"

    vault_from_env = os.environ.get("ORCH_VAULT", "")

    if not config_path.exists():
        if vault_from_env:
            return OrchConfig(vault=vault_from_env)
        raise ValueError("No .orch/config.yaml found and ORCH_VAULT not set")

    with open(config_path) as f:
        data = yaml.safe_load(f) or {}

    vault = vault_from_env or data.get("vault", "")
    if not vault:
        raise ValueError("No vault configured in .orch/config.yaml or ORCH_VAULT")

    return OrchConfig(
        vault=vault,
        agent=data.get("agent", "claude"),
        base_branch=data.get("base_branch", "main"),
        pr_target_branch=data.get("pr_target_branch", "main"),
    )
