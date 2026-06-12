.PHONY: all build install install-cli install-tui deploy test test-fast test-compile lint lint-fixtures lint-install clean kill-daemons update e2e-local-host-worker e2e-remote-smoke e2e-backend-smoke e2e-pr-ci e2e-target-host-worker e2e-target-host-worker-local e2e-zeus-full-flow e2e-run-control-local e2e-run-control-zeus e2e-run-control-matrix
.DEFAULT_GOAL := install

BINARY_NAME := orch
INSTALL_DIR := $(HOME)/.local/bin
UNAME_S := $(shell uname -s)
TEST_PKGS ?= ./...
TEST_TIMEOUT ?= 20m
TEST_GOGC ?= 50
TEST_GOMEMLIMIT ?= 2GiB
TEST_MAX_FD ?= 256
TEST_RUN ?= .
TEST_LOCK_DIR ?= /tmp/orch-go-test.lock
SEMGREP ?= $(shell command -v semgrep 2>/dev/null || echo $(HOME)/.local/bin/semgrep)

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

# Full deploy: local install + remote binary install + restart remote
# master/worker + restart local worker, in dependency order.
# Override hosts via env: REMOTE_HOST=zeus MASTER_ADDR=zeus:7777 make deploy
deploy:
	./scripts/deploy-all.sh

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
	@if ! mkdir $(TEST_LOCK_DIR) 2>/dev/null; then \
		echo "another test run is active ($(TEST_LOCK_DIR))"; \
		exit 1; \
	fi; \
	trap 'rmdir $(TEST_LOCK_DIR)' EXIT INT TERM; \
	ulimit -n $(TEST_MAX_FD); \
	ORCH_SAFE_CLI_TEST=1 GOGC=$(TEST_GOGC) GOMEMLIMIT=$(TEST_GOMEMLIMIT) go test -run '$(TEST_RUN)' -p 1 -parallel 1 -timeout $(TEST_TIMEOUT) $(TEST_PKGS)

test-fast:
	@if ! mkdir $(TEST_LOCK_DIR) 2>/dev/null; then \
		echo "another test run is active ($(TEST_LOCK_DIR))"; \
		exit 1; \
	fi; \
	trap 'rmdir $(TEST_LOCK_DIR)' EXIT INT TERM; \
	ORCH_SAFE_CLI_TEST=1 go test -p 1 -parallel 1 $(TEST_PKGS)

test-compile:
	@if ! mkdir $(TEST_LOCK_DIR) 2>/dev/null; then \
		echo "another test run is active ($(TEST_LOCK_DIR))"; \
		exit 1; \
	fi; \
	trap 'rmdir $(TEST_LOCK_DIR)' EXIT INT TERM; \
	ORCH_SAFE_CLI_TEST=1 GOGC=$(TEST_GOGC) GOMEMLIMIT=$(TEST_GOMEMLIMIT) go test -run '^$$' -p 1 -parallel 1 -timeout $(TEST_TIMEOUT) $(TEST_PKGS)

lint: lint-fixtures
	@test -x "$(SEMGREP)" || uv tool install semgrep
	$(SEMGREP) test .semgrep/typed-id-rules
	$(SEMGREP) test .semgrep/run-status-write-surface
	$(SEMGREP) --error --config .semgrep/ ./internal/ --exclude='*_test.go'
	$(MAKE) -C orch-monitor-tui lint
	$(MAKE) -C orch-monitor-tui lint-test

lint-fixtures:
	@test -x "$(SEMGREP)" || uv tool install semgrep
	@tmp=$$(mktemp); \
	if ! "$(SEMGREP)" --config .semgrep/architecture.yaml --json test/semgrep/internal/daemon/fail_fast_bad.go > "$$tmp"; then \
		cat "$$tmp"; \
		rm -f "$$tmp"; \
		exit 1; \
	fi; \
	findings=$$(grep -o '"check_id"' "$$tmp" | wc -l | tr -d ' '); \
	rm -f "$$tmp"; \
	if [ "$$findings" -lt 20 ]; then \
		echo "expected fail_fast_bad.go to produce at least 20 semgrep findings, got $$findings"; \
		exit 1; \
	fi; \
	echo "semgrep bad fixture produced $$findings expected findings"
	$(SEMGREP) --error --config .semgrep/architecture.yaml test/semgrep/internal/daemon/fail_fast_ok.go

lint-install:
	uv tool install semgrep
	uv tool install pre-commit
	pre-commit install

clean:
	rm -f $(BINARY_NAME)

e2e-local-host-worker:
	./scripts/e2e-master-worker-client-local.sh

e2e-remote-smoke:
	./scripts/e2e-master-worker-client-remote-smoke.sh

e2e-backend-smoke:
	./scripts/e2e-backend-matrix-smoke.sh

e2e-pr-ci:
	./scripts/e2e-pr-ci.sh

e2e-target-host-worker:
	./scripts/e2e-master-worker-client-target.sh

e2e-target-host-worker-local:
	./scripts/e2e-master-worker-client-target-local.sh

e2e-zeus-full-flow:
	./scripts/e2e-master-worker-client-zeus.sh

e2e-run-control-local:
	./scripts/e2e-run-control-local.sh

e2e-run-control-zeus:
	./scripts/e2e-run-control-zeus.sh

e2e-run-control-matrix:
	./scripts/e2e-run-control-matrix.sh
