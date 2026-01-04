.PHONY: all build install test clean
.DEFAULT_GOAL := install

BINARY_NAME := orch
INSTALL_DIR := $(HOME)/.local/bin
UNAME_S := $(shell uname -s)

all: install

build:
	go build -o $(BINARY_NAME) ./cmd/orch

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
ifneq ($(UNAME_S),)
	@if [ "$(UNAME_S)" = "Darwin" ]; then \
		codesign --force --sign - "$(INSTALL_DIR)/$(BINARY_NAME)"; \
	fi
endif
	@$(INSTALL_DIR)/$(BINARY_NAME) daemon-restart 2>/dev/null || true

test:
	go test ./...

clean:
	rm -f $(BINARY_NAME)
