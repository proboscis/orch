"""orch-monitor-tui の rootdir の conftest — 広い test 走行の受付への委譲ちょうど(fixture は持たない)。

正本 = ADR-DOTFILES-027 law broad-test-runs-need-an-explicit-grant(R-9c56a2dd)。判定(広いか)・
30 分の窓の合算・許可の消費は **正本 1 点**(~/dotfiles/agent/tests/broad_run_admission.py)が持ち、
値の宣言は repo に 1 枚だけ(../.agents/land-queue.toml の [test-admission])。受付は対象の path から
.agents/land-queue.toml を持つ repo の根まで遡って方策を引くので、この面のためにもう 1 枚の宣言を
置かない。判定の写しもここへ置かない。中身は repo の root の conftest.py と同じ形。

⚠ **repo の root の 1 本では、この面に届かない**。orch-monitor-tui/ は自前の pyproject.toml に
[tool.pytest.ini_options](testpaths = ["tests"])を持つので、ここで撃った走行の rootdir は
**この dir** になり、pytest は rootdir より上へ conftest を遡らない = repo の root の 1 本は
読まれない。だから同じ形の 2 本目がここに要る。

⚠ この面が、この repo で本当に広い走行ができる面(tests/ = 300 本規模 ≫ per_run_max 100)。
`uv run --extra test pytest tests/` は許可なしでは本体を 1 件も走らせずに断られる。
repo の root の 1 本が守る docs/adr は 7 本で、受付は素通しする。

⚠ 宣言(land-queue.toml の表)と委譲(この file)は対でしか効かない。表だけ消すと
read_policy → PolicyUndeclared → pytest.UsageError(rc 4)で焦点走まで全滅する(fail-closed)。
戻す時は commit ごと revert(two-way door)。

受付は agora の道具立てが在る宿でだけ立つ。clone しかない機体では 1 行名乗って素通しする。
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path
from types import ModuleType

import pytest

_ADMISSION_CANON = Path.home() / "dotfiles" / "agent" / "tests" / "broad_run_admission.py"
#: 固定の module 名 — 同じ走行の中で道具立て側の結線と同じ個体を見る(窓と印を共有する)。
_ADMISSION_MODULE = "ai_broad_run_admission"


def _broad_run_admission() -> ModuleType | None:
    """受付の正本を読む(無ければ None)。"""
    module = sys.modules.get(_ADMISSION_MODULE)
    if module is not None:
        return module
    if not _ADMISSION_CANON.exists():
        return None
    spec = importlib.util.spec_from_file_location(_ADMISSION_MODULE, _ADMISSION_CANON)
    if spec is None or spec.loader is None:
        return None
    module = importlib.util.module_from_spec(spec)
    sys.modules[_ADMISSION_MODULE] = module  # dataclass が sys.modules を読む
    spec.loader.exec_module(module)
    return module


def pytest_collection_finish(session: pytest.Session) -> None:
    """収集の後・本体の前の受付(広い選択なら許可を 1 回消費できた時だけ走らせる)。"""
    admission = _broad_run_admission()
    if admission is None:
        sys.stderr.write(
            f"test-admission: 受付の正本({_ADMISSION_CANON})がこの宿に無いので素通し"
            " — 広い走行の許可は求めない\n"
        )
        return
    admission.collection_finish(session)
