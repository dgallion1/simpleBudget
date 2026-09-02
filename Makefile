# Cross-platform Makefile for budget2
# Works on Linux, macOS, and Windows (with GNU Make)

BINARY := budget2
PORT := 8080
GO_VERSION := 1.26.1
FUZZTIME ?= 30s

# Version information
VERSION := 1.0.0
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# git describe runs in the build cwd, so a worktree build stamps the
# worktree's own HEAD. Do NOT rely on Go's buildvcs stamp instead: for
# builds under .claude/worktrees/* it records the PARENT checkout's HEAD
# and dirty flag (Go walks up past the worktree's .git file), which is
# exactly the staleness confusion this fingerprint exists to remove.
COMMIT := $(shell git describe --always --dirty 2>/dev/null || echo unknown)

# Ldflags: -s strips symbol table, -w strips DWARF debug info
# Also inject version, build time, and commit fingerprint
LDFLAGS := -ldflags="-s -w -X budget2/internal/version.Version=$(VERSION) -X budget2/internal/version.BuildTime=$(BUILD_TIME) -X budget2/internal/version.Commit=$(COMMIT)"

# Detect OS and architecture for platform-specific commands
ifeq ($(OS),Windows_NT)
    BINARY_EXT := .exe
    RM := del /Q
    RMDIR := rmdir /S /Q
    MKDIR := mkdir
    NULL := nul
    GO_OS := windows
    GO_ARCH := amd64
    GO_EXT := .zip
    HOME_DIR := $(USERPROFILE)
else
    BINARY_EXT :=
    RM := rm -f
    RMDIR := rm -rf
    MKDIR := mkdir -p
    NULL := /dev/null
    UNAME_S := $(shell uname -s)
    UNAME_M := $(shell uname -m)
    ifeq ($(UNAME_S),Darwin)
        GO_OS := darwin
    else
        GO_OS := linux
    endif
    ifeq ($(UNAME_M),arm64)
        GO_ARCH := arm64
    else ifeq ($(UNAME_M),aarch64)
        GO_ARCH := arm64
    else
        GO_ARCH := amd64
    endif
    GO_EXT := .tar.gz
    HOME_DIR := $(HOME)
endif

# Go installation directory and binary path
GO_INSTALL_DIR := $(HOME_DIR)/.local/go
GO_LOCAL := $(GO_INSTALL_DIR)/bin/go$(BINARY_EXT)

# Use system Go if available, otherwise use locally installed Go
GO_SYSTEM := $(shell command -v go 2>/dev/null)
ifdef GO_SYSTEM
    GO := $(GO_SYSTEM)
else ifneq (,$(wildcard $(GO_LOCAL)))
    GO := $(GO_LOCAL)
else
    GO := $(GO_LOCAL)
    NEED_GO_INSTALL := 1
endif

# Allow manual override: make GO=/path/to/go build
ifdef GO_OVERRIDE
    GO := $(GO_OVERRIDE)
    NEED_GO_INSTALL :=
endif

.PHONY: all build run dev clean test test-unit test-integration test-coverage fmt lint tidy deps validate validate-v watch vendor-js css css-verify build-all build-linux build-windows build-darwin help install-go check-go release release-snapshot vet static vuln race fuzz check check-full

all: build

check: vet static vuln css-verify test ## Run quality pipeline (pre-commit; race excluded — opt in via `make check-full` or `make race`)
	@echo "✓ all checks passed"

check-full: check race ## Run full quality pipeline including race detector (CI / pre-PR)
	@echo "✓ all checks (incl. race) passed"

# Display available targets
help:
	@echo "Available targets:"
	@echo "  build          - Build for current platform"
	@echo "  run            - Build and run the server"
	@echo "  dev            - Run without building binary"
	@echo "  test           - Run all tests"
	@echo "  test-unit      - Run unit tests only"
	@echo "  test-coverage  - Generate coverage report"
	@echo "  vet            - Run go vet"
	@echo "  static         - Run staticcheck"
	@echo "  vuln           - Run govulncheck"
	@echo "  race           - Run tests with race detector"
	@echo "  fuzz           - Run fuzz tests (FUZZTIME=30s, PKG=./path/to/package)"
	@echo "  clean          - Remove build artifacts"
	@echo "  build-all      - Build for all platforms"
	@echo "  build-linux    - Build for Linux"
	@echo "  build-windows  - Build for Windows"
	@echo "  build-darwin   - Build for macOS"
	@echo "  watch          - Run with hot reload (requires air)"
	@echo "  validate       - Validate running server"
	@echo "  vendor-js      - Download JS dependencies"
	@echo "  css            - Rebuild web/static/css/tailwind.css (commit the result)"
	@echo "  css-verify     - Fail if the committed tailwind.css is stale"
	@echo "  install-go     - Install Go $(GO_VERSION) locally"
	@echo "  release        - Create and push a release tag (usage: make release v=1.0.0)"
	@echo "  release-snapshot - Test release locally without pushing"
	@echo "  docs-api       - Generate Markdown API docs (gomarkdoc) into docs/api/"
	@echo ""
	@echo "Go: $(GO)"

# Install Go locally if not available
install-go:
ifeq ($(OS),Windows_NT)
	@echo "Installing Go $(GO_VERSION) for Windows..."
	@if not exist "$(GO_INSTALL_DIR)" mkdir "$(GO_INSTALL_DIR)"
	@powershell -Command "Invoke-WebRequest -Uri 'https://go.dev/dl/go$(GO_VERSION).windows-amd64.zip' -OutFile '$(TEMP)\go.zip'"
	@powershell -Command "Expand-Archive -Path '$(TEMP)\go.zip' -DestinationPath '$(HOME_DIR)\.local' -Force"
	@del "$(TEMP)\go.zip"
	@echo "Go installed to $(GO_INSTALL_DIR)"
else
	@echo "Installing Go $(GO_VERSION) for $(GO_OS)/$(GO_ARCH)..."
	@$(MKDIR) "$(HOME_DIR)/.local"
	@curl -fsSL "https://go.dev/dl/go$(GO_VERSION).$(GO_OS)-$(GO_ARCH).tar.gz" -o "/tmp/go.tar.gz"
	@rm -rf "$(GO_INSTALL_DIR)"
	@tar -C "$(HOME_DIR)/.local" -xzf "/tmp/go.tar.gz"
	@rm "/tmp/go.tar.gz"
	@echo "Go installed to $(GO_INSTALL_DIR)"
	@echo "Using: $(GO_LOCAL)"
endif

# Check if Go needs to be installed and install if necessary
check-go:
ifdef NEED_GO_INSTALL
	@echo "Go not found. Installing Go $(GO_VERSION)..."
	@$(MAKE) install-go
endif

build: check-go
	$(GO) build $(LDFLAGS) -o $(BINARY)$(BINARY_EXT) ./cmd/server

run: build
	./$(BINARY)$(BINARY_EXT)

dev: check-go
	$(GO) run ./cmd/server

clean:
ifeq ($(OS),Windows_NT)
	-$(RM) $(BINARY).exe 2>$(NULL)
	-$(RMDIR) dist 2>$(NULL)
else
	$(RM) $(BINARY)
	$(RMDIR) dist
endif
	$(GO) clean

# Node.js is required by one test, TestSyncWarnings_ClientRegressionHarness
# (internal/handlers/accounts/warnings_client_regression_test.go), which
# shells out to node to execute accounts.html's client-side syncWarnings()
# script and catch dismissal-ordering regressions (ACCESSIBILITY.md point
# 16) that no Go-only test can see. That guard lives in the test itself,
# not here: the test FAILS (not skips) when node is absent, unless the
# developer opts out with BUDGET2_ALLOW_SKIP_JS (see the test's doc
# comment and README.md's Testing section). Earlier revisions of this
# Makefile tried to police every target that runs `go test` with a
# check-node prerequisite, hand-listed and then discovered by scanning
# this file's text; both were evadable and both have been removed. No
# target below requires node -- the test does.
#
# `go test` result caching is the one gap the test itself cannot close:
# `go test` keys its cache in part on the literal PATH string, and on
# Debian/Ubuntu (and other distros whose package manager installs node into
# a directory that is already on PATH, e.g. /usr/bin) removing node does not
# change that string, so a stale cached PASS can replay and this guard never
# fires -- see the test file's doc comment and README.md's Testing section
# for the full explanation. Every target below that runs this package's
# tests therefore re-runs internal/handlers/accounts a second time with
# -count=1 to force a real, uncached execution; that package takes well
# under a second, so the cost is negligible. This is a fixed, explicit
# rerun of one named package, not a scan of this file or any target for
# `go test` invocations.
ACCOUNTS_PKG := ./internal/handlers/accounts

test: check-go
	$(GO) test ./...
	$(GO) test -count=1 $(ACCOUNTS_PKG)

vet: check-go
	$(GO) vet ./...

static:
	staticcheck ./...

vuln:
	govulncheck ./...

race: check-go
	$(GO) test -race ./...
	$(GO) test -race -count=1 $(ACCOUNTS_PKG)

fuzz: check-go
ifeq ($(strip $(PKG)),)
	@packages="$$(for pkg in $$($(GO) list ./...); do dir=$$($(GO) list -f '{{.Dir}}' $$pkg); if grep -l -E '^func Fuzz[A-Za-z0-9_]*\(' "$$dir"/*_test.go >/dev/null 2>&1; then echo $$pkg; fi; done)"; \
	if [ -z "$$packages" ]; then \
		echo "No fuzz tests found. Run 'make fuzz PKG=./path/to/package' after adding a Fuzz test."; \
	else \
		for pkg in $$packages; do \
			echo "Running fuzz tests in $$pkg"; \
			$(GO) test -fuzz=Fuzz -fuzztime=$(FUZZTIME) -run=^$$ $$pkg || exit $$?; \
		done; \
	fi
else
	$(GO) test -fuzz=Fuzz -fuzztime=$(FUZZTIME) -run=^$$ $(PKG)
endif

test-unit: check-go
	$(GO) test -v ./internal/...
	$(GO) test -count=1 -v $(ACCOUNTS_PKG)

test-integration: check-go
	$(GO) test -v ./cmd/server/...

test-coverage: check-go
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) test -count=1 $(ACCOUNTS_PKG)
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

fmt: check-go
	$(GO) fmt ./...
ifeq ($(OS),Windows_NT)
	@where gofumpt >$(NULL) 2>&1 && gofumpt -w . || echo "gofumpt not found, skipping"
else
	@command -v gofumpt >/dev/null 2>&1 && gofumpt -w . || echo "gofumpt not found, skipping"
endif

lint:
	golangci-lint run

# Generate per-package Markdown API docs from Go doc comments.
# Install: go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest
docs-api: ## Generate Markdown API docs for all Go packages (docs/api/)
	@command -v gomarkdoc >/dev/null 2>&1 || { echo "gomarkdoc not installed. Run: go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest"; exit 1; }
	@$(MKDIR) docs/api
	# Repository details are pinned rather than auto-detected. gomarkdoc
	# derives source links from the git context it happens to find, so the
	# same version emits links from a normal checkout and none from a git
	# worktree — making the committed tree flip depending on who regenerated
	# it last. Passing these explicitly makes the output identical everywhere.
	gomarkdoc --repository.url https://github.com/dgallion1/simpleBudget \
		--repository.default-branch master \
		--repository.path / \
		-o 'docs/api/{{.ImportPath}}.md' ./cmd/... ./internal/...
	@./scripts/gen-api-index.sh
	@echo "API docs written to docs/api/ (start at docs/api/README.md)"

tidy: check-go
	$(GO) mod tidy

deps: check-go
	$(GO) get github.com/go-chi/chi/v5
	$(GO) get github.com/xuri/excelize/v2
	$(GO) mod tidy

# Development with hot reload (requires air to be installed)
# Install air: go install github.com/air-verse/air@latest
watch: check-go
	air

# Tailwind CSS
#
# web/static/css/tailwind.css is a committed build artifact — it is what lets a
# clean checkout render styled with no network (see tailwind.config.js). Being
# committed, it can go stale: add a class to a template, forget to regenerate,
# and the page renders correctly for whoever still has the old CSS cached and
# wrong for everyone else, with nothing failing to build. These two targets
# make regenerating it a command rather than a recipe to copy out of a comment,
# and make staleness a test failure.
#
# The standalone CLI is used rather than `npx tailwindcss`: it is a single
# binary, so this needs no npm install and leaves no node_modules or
# package.json in the repo root to clean up afterwards. It lands in tmp/, which
# is gitignored.
#
# Pinned to 3.4.17 — the version cdn.tailwindcss.com was serving when the
# runtime CDN was dropped. Bumping it is a visual change, not a dependency
# bump: rebuild and look at the pages.
TAILWIND_VERSION := 3.4.17
ifeq ($(GO_OS),darwin)
    TAILWIND_OS := macos
else
    TAILWIND_OS := $(GO_OS)
endif
ifeq ($(GO_ARCH),amd64)
    TAILWIND_ARCH := x64
else
    TAILWIND_ARCH := $(GO_ARCH)
endif
TAILWIND_BIN := tmp/tailwindcss-$(TAILWIND_VERSION)$(BINARY_EXT)
TAILWIND_URL := https://github.com/tailwindlabs/tailwindcss/releases/download/v$(TAILWIND_VERSION)/tailwindcss-$(TAILWIND_OS)-$(TAILWIND_ARCH)$(BINARY_EXT)
TAILWIND_ARGS := -c tailwind.config.js -i web/static/css/tailwind.src.css

$(TAILWIND_BIN):
	$(MKDIR) tmp
	curl -fL $(TAILWIND_URL) -o $(TAILWIND_BIN)
ifneq ($(OS),Windows_NT)
	chmod +x $(TAILWIND_BIN)
endif

# Rebuild the committed stylesheet. Run after adding a class name that is not
# already in it, and commit the result alongside the change.
css: $(TAILWIND_BIN)
	./$(TAILWIND_BIN) $(TAILWIND_ARGS) -o web/static/css/tailwind.css --minify
	@echo "web/static/css/tailwind.css rebuilt - commit it"

# Rebuild to a scratch file and compare. Complements swarm/t7-coverage.sh:
# that script asks whether the committed CSS covers the class tokens it can
# find in the templates, this one asks whether the committed CSS is what the
# current tree actually builds.
css-verify: $(TAILWIND_BIN)
	./$(TAILWIND_BIN) $(TAILWIND_ARGS) -o tmp/tailwind.check.css --minify
	@cmp -s tmp/tailwind.check.css web/static/css/tailwind.css \
		&& echo "tailwind.css is up to date" \
		|| { echo "ERROR: web/static/css/tailwind.css is stale - run 'make css' and commit the result"; exit 1; }

# Download vendor JS libraries (requires curl)
vendor-js:
ifeq ($(OS),Windows_NT)
	@if not exist web\static\vendor mkdir web\static\vendor
else
	$(MKDIR) web/static/vendor
endif
	curl -L https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js -o web/static/vendor/htmx.min.js
	curl -L https://cdn.plot.ly/plotly-2.35.2.min.js -o web/static/vendor/plotly.min.js

# Validate a running server
validate: check-go
	$(GO) run ./cmd/validate -url http://localhost:$(PORT)

# Validate with verbose output
validate-v: check-go
	$(GO) run ./cmd/validate -url http://localhost:$(PORT) -v

# Build for all platforms
build-all: build-linux build-windows build-darwin
	@echo "Built all platforms in dist/"

# Build for Linux (size-optimized)
build-linux: check-go
ifeq ($(OS),Windows_NT)
	@if not exist dist mkdir dist
	set CGO_ENABLED=0&& set GOOS=linux&& set GOARCH=amd64&& $(GO) build $(LDFLAGS) -o dist/$(BINARY)-linux-amd64 ./cmd/server
else
	$(MKDIR) dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/$(BINARY)-linux-amd64 ./cmd/server
endif

# Build for Windows
build-windows: check-go
ifeq ($(OS),Windows_NT)
	@if not exist dist mkdir dist
	set CGO_ENABLED=0&& set GOOS=windows&& set GOARCH=amd64&& $(GO) build $(LDFLAGS) -o dist/$(BINARY)-windows-amd64.exe ./cmd/server
else
	$(MKDIR) dist
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/$(BINARY)-windows-amd64.exe ./cmd/server
endif

# Build for macOS (Intel and Apple Silicon)
build-darwin: check-go
ifeq ($(OS),Windows_NT)
	@if not exist dist mkdir dist
	set CGO_ENABLED=0&& set GOOS=darwin&& set GOARCH=amd64&& $(GO) build $(LDFLAGS) -o dist/$(BINARY)-darwin-amd64 ./cmd/server
	set CGO_ENABLED=0&& set GOOS=darwin&& set GOARCH=arm64&& $(GO) build $(LDFLAGS) -o dist/$(BINARY)-darwin-arm64 ./cmd/server
else
	$(MKDIR) dist
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/$(BINARY)-darwin-amd64 ./cmd/server
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o dist/$(BINARY)-darwin-arm64 ./cmd/server
endif

# Release targets (requires goreleaser: go install github.com/goreleaser/goreleaser/v2@latest)

# Test release locally without pushing
release-snapshot:
	goreleaser release --snapshot --clean

# Create and push a release tag
# Usage: make release v=1.0.0
release:
ifndef v
	@echo "Error: version not specified. Usage: make release v=1.0.0"
	@exit 1
endif
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: working directory not clean. Commit or stash changes first."; \
		exit 1; \
	fi
	@echo "Creating release v$(v)..."
	git tag -a v$(v) -m "Release v$(v)"
	git push origin v$(v)
	@echo "Release v$(v) pushed. GitHub Actions will build and publish."
