"""Pytest fixture required by doeff-adr deftest enforcement blocks.

The ADR test runtime is deliberately minimal (doeff-adr responsibility
boundary): no env overrides, no daemons, no project handler stacks.
"""

import pytest


@pytest.fixture
def doeff_interpreter():
    def run_program(program, *, env=None):
        from doeff import run

        if env:
            raise ValueError("ADR test runtime does not use env overrides")
        return run(program)

    return run_program
