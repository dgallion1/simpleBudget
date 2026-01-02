GO := ~/go-sdk/go/bin/go
BINARY := budget2
PORT := 8080

.PHONY: all build run dev clean test test-unit test-integration test-coverage fmt lint tidy deps validate validate-start

all: build

build:
	$(GO) build -o $(BINARY) ./cmd/server

run: build
	./$(BINARY)

dev:
	$(GO) run ./cmd/server

clean:
	rm -f $(BINARY)
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
	gofumpt -w .

lint:
	golangci-lint run

tidy:
	$(GO) mod tidy

deps:
	$(GO) get github.com/go-chi/chi/v5
	$(GO) get github.com/xuri/excelize/v2
	$(GO) mod tidy

# Development with hot reload (installs air if needed)
watch:
	@test -x ~/go/bin/air || $(GO) install github.com/air-verse/air@latest
	~/go/bin/air

# Download vendor JS libraries
vendor-js:
	mkdir -p web/static/vendor
	curl -L https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js -o web/static/vendor/htmx.min.js
	curl -L https://cdn.plot.ly/plotly-2.35.2.min.js -o web/static/vendor/plotly.min.js

# Validate a running server
validate:
	$(GO) run ./cmd/validate -url http://localhost:$(PORT)

# Validate with verbose output
validate-v:
	$(GO) run ./cmd/validate -url http://localhost:$(PORT) -v
