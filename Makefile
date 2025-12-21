.PHONY: build install test clean

BINARY_NAME := orch
INSTALL_DIR := $(HOME)/.local/bin

build:
	go build -o $(BINARY_NAME) ./cmd/orch

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)

test:
	go test ./...

clean:
	rm -f $(BINARY_NAME)
