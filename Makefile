# Cross-platform Makefile for budget2
# Works on Linux, macOS, and Windows (with GNU Make)

BINARY := budget2
PORT := 8080
GO_VERSION := 1.25.0

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

.PHONY: all build run dev clean test test-unit test-integration test-coverage fmt lint tidy deps validate validate-v watch vendor-js build-all build-linux build-windows build-darwin help install-go check-go

all: build

# Display available targets
help:
	@echo "Available targets:"
	@echo "  build          - Build for current platform"
	@echo "  run            - Build and run the server"
	@echo "  dev            - Run without building binary"
	@echo "  test           - Run all tests"
	@echo "  test-unit      - Run unit tests only"
	@echo "  test-coverage  - Generate coverage report"
	@echo "  clean          - Remove build artifacts"
	@echo "  build-all      - Build for all platforms"
	@echo "  build-linux    - Build for Linux"
	@echo "  build-windows  - Build for Windows"
	@echo "  build-darwin   - Build for macOS"
	@echo "  watch          - Run with hot reload (requires air)"
	@echo "  validate       - Validate running server"
	@echo "  vendor-js      - Download JS dependencies"
	@echo "  install-go     - Install Go $(GO_VERSION) locally"
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
	$(GO) build -o $(BINARY)$(BINARY_EXT) ./cmd/server

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

test: check-go
	$(GO) test -v ./...

test-unit: check-go
	$(GO) test -v ./internal/...

test-integration: check-go
	$(GO) test -v ./cmd/server/...

test-coverage: check-go
	$(GO) test -coverprofile=coverage.out ./...
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

# Size-optimized ldflags: -s strips symbol table, -w strips DWARF debug info
LDFLAGS := -ldflags="-s -w"

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
