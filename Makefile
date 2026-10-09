BINARY    := hello-mondoo
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DIST      := dist
LDFLAGS   := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

# Keep in sync with .github/workflows/ci.yml
GOLANGCI_LINT_VERSION := v2.14.0
GOVULNCHECK_VERSION   := v1.8.0
SEMGREP_IMAGE         := semgrep/semgrep:1.180.0@sha256:529ee8a277ec8adc5b534d7c74eea0a47e9de21d62852b6ba7ac6ba9566845c3

GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK   ?= go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

export CGO_ENABLED := 0

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build for the host platform into ./dist
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) .

.PHONY: run
run: build ## Build and run (override port with HTTP_PORT=9090 make run)
	./$(DIST)/$(BINARY)

.PHONY: test
test: ## Run tests with the race detector and coverage
	CGO_ENABLED=1 go test -race -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

.PHONY: lint
lint: ## Run golangci-lint (same version as CI)
	$(GOLANGCI_LINT) run ./...

.PHONY: fmt
fmt: ## Format code with the golangci-lint formatters
	$(GOLANGCI_LINT) fmt ./...

.PHONY: vuln
vuln: ## Run govulncheck
	$(GOVULNCHECK) ./...

.PHONY: sast
sast: ## Run semgrep in Docker with the CI rulesets
	docker run --rm -v "$(CURDIR):/src" -w /src $(SEMGREP_IMAGE) \
		semgrep scan --config p/golang --config p/secrets --metrics off --error

.PHONY: check
check: lint vuln sast test ## Run every CI check locally

.PHONY: cross
cross: ## Cross-compile all release targets into ./dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=""; \
		[ "$$os" = "windows" ] && ext=".exe"; \
		out="$(DIST)/$(BINARY)_$${os}_$${arch}$${ext}"; \
		echo "building $$out"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o "$$out" . || exit 1; \
	done

.PHONY: clean
clean: ## Remove build and test output
	rm -rf $(DIST) coverage.out
