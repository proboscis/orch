"""repo の root の conftest — 広い test 走行の受付への委譲ちょうど(fixture は持たない)。

正本 = ADR-DOTFILES-027 law broad-test-runs-need-an-explicit-grant(R-9c56a2dd)。広い選択の
走行は、頭脳が発行した期限つきの許可を 1 回消費できた時だけ本体を走らせる。判定(広いか)・
30 分の窓の合算・許可の消費は **正本 1 点**(~/dotfiles/agent/tests/broad_run_admission.py)が
持ち、この repo は .agents/land-queue.toml の [test-admission] で **値だけ** を宣言する。
判定の写しをここへ置かない — 写すと方策を動かした日に片方だけ古い答えを返す。
手本 = agora-controllers / argus の root conftest.py(先に据えた区画と同じ形)。

⚠ **方策(land-queue.toml の表)と委譲(この file)は対でしか効かない**。表だけ消してこの file を
残すと read_policy → PolicyUndeclared → pytest.UsageError(rc 4)で、本数に依らず焦点走まで
全部落ちる(受付の fail-closed)。だから宣言とこの file は 1 commit で出し、戻す時も commit ごと
revert する(two-way door)。

⚠ **root 1 本で docs/adr に届く理由**: この repo は root の pyproject.toml に
[tool.pytest.ini_options](testpaths = ["docs/adr"])を持つので rootdir = repo の root で固定され、
どの走行(1 file の名指し・-k の絞り・[gate].full の `uv run pytest docs/adr`)でもこの file が
読まれる。

⚠ **root 1 本では orch-monitor-tui に届かない理由**: orch-monitor-tui/ は自前の pyproject.toml に
[tool.pytest.ini_options] を持つので、そこで撃った走行の rootdir は **orch-monitor-tui/** になり、
repo の root のこの file は読まれない(pytest は rootdir より上へ conftest を遡らない)。
だから同じ形の 2 本目を orch-monitor-tui/conftest.py に置いてある。この repo で本当に広い走行が
できるのはあちらの面(tests/ = 300 本規模)なので、2 本目を消すと法の目的が抜ける。

受付は agora の道具立てが在る宿でだけ立つ。clone しかない機体には許可の口も頭脳も無いので、
1 行名乗って素通しする(そこで止めると test が 1 本も走れない)。道具立てが在る宿では
fail-closed(方策が読めない / 許可が得られない = 本体を 1 件も走らせずに終わる)。
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
