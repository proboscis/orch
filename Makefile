.PHONY: all build install install-cli install-tui test clean restart-daemons update
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

# Install everything and restart daemons
install: install-cli install-tui restart-daemons

# Restart all orch daemons
restart-daemons:
	@echo "Restarting orch daemons..."
	@for pid in $$(pgrep -f "orch daemon" 2>/dev/null); do \
		vault=$$(ps -p $$pid -o args= | sed -n 's/.*--vault \([^ ]*\).*/\1/p'); \
		if [ -n "$$vault" ]; then \
			echo "Restarting daemon for vault: $$vault"; \
			$(INSTALL_DIR)/$(BINARY_NAME) --vault "$$vault" daemon-restart 2>/dev/null || true; \
		fi; \
	done
	@echo "Done."

# Pull from main and reinstall everything
update:
	git pull origin main
	$(MAKE) install

test:
	go test ./...

clean:
	rm -f $(BINARY_NAME)
