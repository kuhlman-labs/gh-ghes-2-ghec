# Variables
BINARY_NAME=gh-ghes-2-ghec
MAIN_FILE=main.go
VERSION ?= $(shell git describe --tags --always --dirty)
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X github.com/kuhlman-labs/gh-ghes-2-ghec/internal/version.Version=${VERSION} -X github.com/kuhlman-labs/gh-ghes-2-ghec/internal/version.BuildTime=${BUILD_TIME}"
GO_FILES=$(shell find . -name "*.go" -type f -not -path "./vendor/*")
CSS_DIR=static/css

.PHONY: all build clean test lint vet fmt docker docker-run help run css-deps css-build css-lint css-clean

all: clean fmt lint test css-build build

# CSS Dependencies - Install Node.js dependencies for CSS building
css-deps:
	@echo "Installing CSS dependencies..."
	cd $(CSS_DIR) && npm install

# CSS Build - Build and minify CSS files
css-build: css-deps
	@echo "Building CSS..."
	cd $(CSS_DIR) && npm run build

# CSS Lint - Lint CSS files
css-lint: css-deps
	@echo "Linting CSS..."
	cd $(CSS_DIR) && npm run lint

# CSS Clean - Clean CSS build artifacts
css-clean:
	@echo "Cleaning CSS build artifacts..."
	rm -rf $(CSS_DIR)/dist/*
	rm -rf $(CSS_DIR)/node_modules

# Build the application (now includes CSS build)
build: css-build
	go build -o $(BINARY_NAME) $(LDFLAGS) $(MAIN_FILE)

# Clean build files (now includes CSS)
clean: css-clean
	rm -f $(BINARY_NAME)
	go clean

# Run tests
test:
	go test -v ./...

# Run linter (Go)
lint:
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

# Run Go Sec
sec:
	go run -buildvcs=false github.com/securego/gosec/v2/cmd/gosec@latest ./...

# Run go vet
vet:
	go vet ./...

# Format the code
fmt:
	gofmt -s -w $(GO_FILES)

# Build and run the server with dashboard enabled
run: build
	./$(BINARY_NAME)

# Build docker image
docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg BUILD_TIME=$(BUILD_TIME) -t $(BINARY_NAME):$(VERSION) .

# Run docker container
docker-run:
	docker run --rm -it $(BINARY_NAME):$(VERSION)

# Install the application
install: build
	mv $(BINARY_NAME) $(GOPATH)/bin/$(BINARY_NAME)

# Development target - build CSS and run with file watching
dev: css-build
	./$(BINARY_NAME)

# Show help
help:
	@echo "Available commands:"
	@echo "  make              : Build the application after running format, lint, test, and CSS build"
	@echo "  make build        : Build the application (includes CSS build)"
	@echo "  make clean        : Clean build files (includes CSS)"
	@echo "  make test         : Run tests"
	@echo "  make lint         : Run linter"
	@echo "  make vet          : Run go vet"
	@echo "  make fmt          : Format code"
	@echo "  make run          : Build and run the server with dashboard enabled"
	@echo "  make docker       : Build docker image"
	@echo "  make docker-run   : Run docker container"
	@echo "  make install      : Install the application"
	@echo "  make css-build    : Build CSS files"
	@echo "  make css-lint     : Lint CSS files"
	@echo "  make css-clean    : Clean CSS build artifacts"
	@echo "  make css-deps     : Install CSS dependencies"
	@echo "  make dev          : Build and run for development" 