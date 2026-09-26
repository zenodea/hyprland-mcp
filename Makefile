VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PREFIX ?= $(HOME)/.local

build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o hyprland-mcp .

test:
	go vet ./...
	go test ./...

install: build
	install -Dm755 hyprland-mcp $(PREFIX)/bin/hyprland-mcp

.PHONY: build test install
