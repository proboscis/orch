"""Tests for control-agent fields in Python monitor config loading."""

from pathlib import Path

from orch_monitor.config import Config


def test_load_parses_control_agent_fields(tmp_path: Path, monkeypatch):
    repo = tmp_path / "repo"
    orch_dir = repo / ".orch"
    orch_dir.mkdir(parents=True)
    (orch_dir / "config.yaml").write_text(
        "\n".join(
            [
                "agent: opencode",
                "control_agent: claude",
                "control_model: anthropic/claude-opus-4-6",
                "control_model_variant: max",
            ]
        )
        + "\n",
        encoding="utf-8",
    )

    monkeypatch.chdir(repo)
    cfg = Config.load()

    assert cfg.agent == "opencode"
    assert cfg.control_agent == "claude"
    assert cfg.control_model == "anthropic/claude-opus-4-6"
    assert cfg.control_model_variant == "max"
