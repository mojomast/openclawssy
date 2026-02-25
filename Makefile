SHELL := /bin/sh

BINARY := openclawssy
BIN_DIR := bin
CMD_PATH := ./cmd/openclawssy
PKGS := $(shell go list ./... 2>/dev/null)

.PHONY: fmt lint test build test-live-glm

fmt:
	@files=$$(go list -f '{{ range .GoFiles }}{{ $$.Dir }}/{{ . }} {{ end }}' ./... 2>/dev/null); \
	if [ -n "$$files" ]; then gofmt -w $$files; else printf "no go files to format\n"; fi

lint:
	@if [ -n "$(PKGS)" ]; then go vet ./...; else printf "no packages to lint\n"; fi

test:
	@if [ -n "$(PKGS)" ]; then go test ./...; else printf "no packages to test\n"; fi

build:
	@mkdir -p $(BIN_DIR)
	@if [ -d "$(CMD_PATH)" ]; then go build -o $(BIN_DIR)/$(BINARY) $(CMD_PATH); else printf "missing %s\n" "$(CMD_PATH)"; fi

test-live-glm:
	@OPENCLAWSSY_LIVE_GLM=1 go test -tags liveglm -count=1 ./internal/runtime -run TestLiveGLMToolCallsCompleteWithoutLooping
