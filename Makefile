.PHONY: all build install install-cli install-tui test lint lint-install clean kill-daemons update
.DEFAULT_GOAL := install

BINARY_NAME := orch
INSTALL_DIR := $(HOME)/.local/bin
UNAME_S := $(shell uname -s)

all: install

# Build Go binary
build:
	go build -o $(BINARY_NAME) ./cmd/orch

# Install Go CLI + daemon
install-cli: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
ifneq ($(UNAME_S),)
	@if [ "$(UNAME_S)" = "Darwin" ]; then \
		codesign --force --sign - "$(INSTALL_DIR)/$(BINARY_NAME)"; \
	fi
endif

# Install Python TUI
install-tui:
	uv tool install --force --reinstall ./orch-monitor-tui

# Install everything, then kill daemons (they restart on demand)
install: install-cli install-tui kill-daemons

# Kill all orch daemons and opencode servers (clean slate)
kill-daemons:
	@echo "Killing all orch daemons and opencode servers..."
	@pkill -9 -f "orch daemon" 2>/dev/null || true
	@pkill -9 -f "opencode serve" 2>/dev/null || true
	@sleep 1
	@echo "Done. Daemons will restart automatically on next orch command."

# Pull from main and reinstall everything
update:
	git pull origin main
	$(MAKE) install

test:
	go test ./...

lint:
	@command -v semgrep >/dev/null 2>&1 || uv tool install semgrep
	@echo "=== Architecture lint: CLI layer ==="
	semgrep --config .semgrep/ ./internal/cli/ --exclude='*_test.go'
	@echo ""
	@echo "=== Architecture lint: Monitor layer ==="
	semgrep --config .semgrep/ ./internal/monitor/ --exclude='*_test.go'

lint-strict:
	@command -v semgrep >/dev/null 2>&1 || uv tool install semgrep
	semgrep --error --config .semgrep/ ./internal/cli/ ./internal/monitor/ --exclude='*_test.go'

lint-install:
	uv tool install semgrep
	uv tool install pre-commit
	pre-commit install

clean:
	rm -f $(BINARY_NAME)
