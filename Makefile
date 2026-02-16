BINARY := wt
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X github.com/mvwi/wt/internal/cmd.Version=$(VERSION)"

.PHONY: build install test lint clean setup

build:
	go build -trimpath $(LDFLAGS) -o $(BINARY) ./cmd/wt

install:
	go install $(LDFLAGS) ./cmd/wt

test:
	go test ./... -v

lint:
	golangci-lint run ./...

coverage:
	go test ./... -coverprofile=coverage.txt -covermode=atomic
	go tool cover -html=coverage.txt -o coverage.html

setup:
	git config core.hooksPath .githooks
	@echo "Git hooks installed from .githooks/"

clean:
	rm -f $(BINARY) coverage.txt coverage.html
	rm -rf dist/
