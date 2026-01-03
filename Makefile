# Cross-platform Makefile for budget2
# Works on Linux, macOS, and Windows (with GNU Make)

# Use Go from PATH by default, can be overridden: make GO=/path/to/go build
GO ?= go
BINARY := budget2
PORT := 8080

# Detect OS for platform-specific commands
ifeq ($(OS),Windows_NT)
    BINARY_EXT := .exe
    RM := del /Q
    RMDIR := rmdir /S /Q
    MKDIR := mkdir
    NULL := nul
else
    BINARY_EXT :=
    RM := rm -f
    RMDIR := rm -rf
    MKDIR := mkdir -p
    NULL := /dev/null
endif

.PHONY: all build run dev clean test test-unit test-integration test-coverage fmt lint tidy deps validate validate-v watch vendor-js build-all build-linux build-windows build-darwin help

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

build:
	$(GO) build -o $(BINARY)$(BINARY_EXT) ./cmd/server

run: build
	./$(BINARY)$(BINARY_EXT)

dev:
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

test:
	$(GO) test -v ./...

test-unit:
	$(GO) test -v ./internal/...

test-integration:
	$(GO) test -v ./cmd/server/...

test-coverage:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

fmt:
	$(GO) fmt ./...
ifeq ($(OS),Windows_NT)
	@where gofumpt >$(NULL) 2>&1 && gofumpt -w . || echo "gofumpt not found, skipping"
else
	@command -v gofumpt >/dev/null 2>&1 && gofumpt -w . || echo "gofumpt not found, skipping"
endif

lint:
	golangci-lint run

tidy:
	$(GO) mod tidy

deps:
	$(GO) get github.com/go-chi/chi/v5
	$(GO) get github.com/xuri/excelize/v2
	$(GO) mod tidy

# Development with hot reload (requires air to be installed)
# Install air: go install github.com/air-verse/air@latest
watch:
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
validate:
	$(GO) run ./cmd/validate -url http://localhost:$(PORT)

# Validate with verbose output
validate-v:
	$(GO) run ./cmd/validate -url http://localhost:$(PORT) -v

# Size-optimized ldflags: -s strips symbol table, -w strips DWARF debug info
LDFLAGS := -ldflags="-s -w"

# Build for all platforms
build-all: build-linux build-windows build-darwin
	@echo "Built all platforms in dist/"

# Build for Linux (size-optimized)
build-linux:
ifeq ($(OS),Windows_NT)
	@if not exist dist mkdir dist
	set CGO_ENABLED=0&& set GOOS=linux&& set GOARCH=amd64&& $(GO) build $(LDFLAGS) -o dist/$(BINARY)-linux-amd64 ./cmd/server
else
	$(MKDIR) dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/$(BINARY)-linux-amd64 ./cmd/server
endif

# Build for Windows
build-windows:
ifeq ($(OS),Windows_NT)
	@if not exist dist mkdir dist
	set CGO_ENABLED=0&& set GOOS=windows&& set GOARCH=amd64&& $(GO) build $(LDFLAGS) -o dist/$(BINARY)-windows-amd64.exe ./cmd/server
else
	$(MKDIR) dist
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/$(BINARY)-windows-amd64.exe ./cmd/server
endif

# Build for macOS (Intel and Apple Silicon)
build-darwin:
ifeq ($(OS),Windows_NT)
	@if not exist dist mkdir dist
	set CGO_ENABLED=0&& set GOOS=darwin&& set GOARCH=amd64&& $(GO) build $(LDFLAGS) -o dist/$(BINARY)-darwin-amd64 ./cmd/server
	set CGO_ENABLED=0&& set GOOS=darwin&& set GOARCH=arm64&& $(GO) build $(LDFLAGS) -o dist/$(BINARY)-darwin-arm64 ./cmd/server
else
	$(MKDIR) dist
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o dist/$(BINARY)-darwin-amd64 ./cmd/server
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o dist/$(BINARY)-darwin-arm64 ./cmd/server
endif
