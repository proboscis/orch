SEMGREP ?= $(shell command -v semgrep 2>/dev/null || echo $(HOME)/.local/bin/semgrep)

.PHONY: lint lint-test test install dev clean

# Run all linters
lint:
	@test -x "$(SEMGREP)" || uv tool install semgrep
	@echo "Running semgrep architecture rules..."
	$(SEMGREP) --error --config .semgrep/architecture.yaml orch_monitor/
	@echo "Lint passed!"

lint-test:
	@test -x "$(SEMGREP)" || uv tool install semgrep
	@echo "Running semgrep architecture rule tests..."
	uv run python .semgrep/check_architecture_fixtures.py "$(SEMGREP)"

# Run tests
test:
	uv run pytest tests/ -v

# Install dependencies
install:
	uv sync

# Install with dev/test dependencies
dev:
	uv sync --all-extras

# Clean build artifacts
clean:
	rm -rf dist/ build/ *.egg-info/
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	find . -type f -name "*.pyc" -delete 2>/dev/null || true
