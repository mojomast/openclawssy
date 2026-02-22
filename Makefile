SHELL := /bin/sh

BINARY := openclawssy
BIN_DIR := bin
CMD_PATH := ./cmd/openclawssy
PKGS := $(shell go list ./... 2>/dev/null)

.PHONY: fmt fmt-check lint test test-race test-security build smoke ci-quick ci-race ci-security ci

fmt:
	@files=$$(go list -f '{{ range .GoFiles }}{{ $$.Dir }}/{{ . }} {{ end }}' ./... 2>/dev/null); \
	if [ -n "$$files" ]; then gofmt -w $$files; else printf "no go files to format\n"; fi

fmt-check:
	@files=$$(go list -f '{{ range .GoFiles }}{{ $$.Dir }}/{{ . }} {{ end }}' ./... 2>/dev/null); \
	if [ -z "$$files" ]; then \
		printf "no go files to check\n"; \
	else \
		unformatted=$$(gofmt -l $$files); \
		if [ -n "$$unformatted" ]; then \
			printf "unformatted files:\n%s\n" "$$unformatted"; \
			exit 1; \
		fi; \
	fi

lint:
	@if [ -n "$(PKGS)" ]; then go vet ./...; else printf "no packages to lint\n"; fi

test:
	@if [ -n "$(PKGS)" ]; then go test ./...; else printf "no packages to test\n"; fi

test-race:
	@if [ -n "$(PKGS)" ]; then go test -race ./...; else printf "no packages to test\n"; fi

test-security:
	@if [ -n "$(PKGS)" ]; then \
		go test ./internal/channels/http ./internal/policy ./internal/tools -run 'Security|SSRF|Traversal|Symlink|Protected|Auth|Redact|Sandbox|PathTraversal'; \
	else \
		printf "no packages to test\n"; \
	fi

build:
	@mkdir -p $(BIN_DIR)
	@if [ -d "$(CMD_PATH)" ]; then go build -o $(BIN_DIR)/$(BINARY) $(CMD_PATH); else printf "missing %s\n" "$(CMD_PATH)"; fi

smoke: build
	@./$(BIN_DIR)/$(BINARY) doctor

ci-quick: fmt-check lint test smoke

ci-race: test-race

ci-security: test-security

ci: ci-quick ci-security ci-race
