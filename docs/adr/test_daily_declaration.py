"""日次の全体検証の宣言(.agents/land-queue.toml の [gate] full)の検(agora-redesign #3879)。

日次の全体検証は欄 pytest = true の処理ステージで session が 1 本も答えなければカバレッジの欠けに数え、欄の無い処理ステージは
「pytest の実行は未宣言」と記帳する(走らせ器は断らない — Mac の調整役の決定 2026-10-07)。欄の書き忘れはこの repo の検で赤にする。
"""

from __future__ import annotations

import tomllib
from pathlib import Path

_LAND_QUEUE = Path(__file__).resolve().parents[2] / ".agents" / "land-queue.toml"


def test_every_daily_stage_declares_whether_it_runs_pytest() -> None:
    """全部の処理ステージに欄 pytest(true か false)が在る事を確かめるため。"""
    full = tomllib.loads(_LAND_QUEUE.read_text(encoding="utf-8"))["gate"]["full"]
    assert isinstance(full, list) and full, f"[gate] full が処理ステージの列でない(1 本の文字列には欄 pytest を書けない): {full!r}"
    missing = [stage.get("name") for stage in full if not isinstance(stage.get("pytest"), bool)]
    assert not missing, f"[gate] full の処理ステージ {missing} に欄 pytest(true か false)が無い"
