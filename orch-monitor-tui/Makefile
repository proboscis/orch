.PHONY: lint test install dev clean

# Run all linters
lint:
	@echo "Running semgrep architecture rules..."
	semgrep --config .semgrep/architecture.yaml orch_monitor/
	@echo "Lint passed!"

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
