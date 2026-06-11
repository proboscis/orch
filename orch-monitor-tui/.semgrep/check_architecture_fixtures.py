"""Run TUI architecture semgrep fixtures.

Semgrep's built-in test mode is unstable for this repo layout in the currently
installed version, so this script exercises the same fixtures through the JSON
scanner and fails if any required guardrail stops matching.
"""

from __future__ import annotations

import json
import subprocess
import sys
from collections import Counter
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CONFIG = ROOT / ".semgrep" / "architecture.yaml"
FORBIDDEN_FIXTURES = [
    ROOT / ".semgrep" / "architecture.py",
    ROOT / ".semgrep" / "architecture.hy",
]
OK_FIXTURES = [
    ROOT / ".semgrep" / "architecture_ok.py",
    ROOT / ".semgrep" / "architecture_ok.hy",
]
EXPECTED_RULES = {
    "tui-no-git-subprocess",
    "tui-no-local-repo-id-parsing",
    "tui-no-remote-addr-resolution",
    "tui-no-proto-status-remap",
    "tui-no-proto-branch-mux-remap",
    "tui-no-client-monitor-session-name-generation",
    "tui-no-client-side-run-sort",
    "tui-no-client-side-filtering",
    "tui-no-bare-except",
    "hy-tui-no-bare-except",
}


def _run_semgrep(semgrep: str, targets: list[Path]) -> list[dict]:
    cmd = [
        semgrep,
        "--json",
        "--config",
        str(CONFIG),
        *[str(path) for path in targets],
    ]
    result = subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True)
    if result.returncode not in (0, 1):
        raise RuntimeError(result.stderr or result.stdout)
    data = json.loads(result.stdout or "{}")
    return list(data.get("results", []))


def _rule_id(result: dict) -> str:
    check_id = str(result.get("check_id", ""))
    return check_id.rsplit(".", 1)[-1]


def _expected_counts(paths: list[Path]) -> Counter[str]:
    counts: Counter[str] = Counter()
    for path in paths:
        for line in path.read_text().splitlines():
            marker = "ruleid:"
            if marker not in line:
                continue
            rule_id = line.split(marker, 1)[1].strip().split()[0]
            counts[rule_id] += 1
    return counts


def main() -> int:
    semgrep = sys.argv[1] if len(sys.argv) > 1 else "semgrep"
    forbidden_results = _run_semgrep(semgrep, FORBIDDEN_FIXTURES)
    found_counts = Counter(_rule_id(result) for result in forbidden_results)
    found = set(found_counts)
    expected_counts = _expected_counts(FORBIDDEN_FIXTURES)
    missing = EXPECTED_RULES - found
    if missing:
        print("Missing expected semgrep fixture matches:", file=sys.stderr)
        for rule_id in sorted(missing):
            print(f"  - {rule_id}", file=sys.stderr)
        return 1
    undercovered = {
        rule_id: (expected, found_counts.get(rule_id, 0))
        for rule_id, expected in expected_counts.items()
        if found_counts.get(rule_id, 0) < expected
    }
    if undercovered:
        print("Semgrep fixture match counts are lower than ruleid markers:", file=sys.stderr)
        for rule_id, (expected, actual) in sorted(undercovered.items()):
            print(f"  - {rule_id}: expected >= {expected}, got {actual}", file=sys.stderr)
        return 1

    ok_results = _run_semgrep(semgrep, OK_FIXTURES)
    if ok_results:
        print("Thin-view fixtures produced unexpected findings:", file=sys.stderr)
        for result in ok_results:
            path = result.get("path", "")
            start = result.get("start", {})
            print(f"  - {_rule_id(result)} at {path}:{start.get('line')}", file=sys.stderr)
        return 1

    print(
        f"Semgrep fixtures passed: {len(EXPECTED_RULES)} guardrails matched, thin view clean."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
