# Variables
BINARY_NAME=gh-ghes-2-ghec
MAIN_FILE=main.go
VERSION ?= $(shell git describe --tags --always --dirty)
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X github.com/kuhlman-labs/gh-ghes-2-ghec/internal/version.Version=${VERSION} -X github.com/kuhlman-labs/gh-ghes-2-ghec/internal/version.BuildTime=${BUILD_TIME}"
GO_FILES=$(shell find . -name "*.go" -type f -not -path "./vendor/*")

.PHONY: all build clean test lint vet fmt docker docker-run help run

all: clean fmt lint test build

# Build the application
build:
	go build -o $(BINARY_NAME) $(LDFLAGS) $(MAIN_FILE)

# Clean build files
clean:
	rm -f $(BINARY_NAME)
	go clean

# Run tests
test:
	go test -v ./...

# Run linter
lint:
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

# Run Go Sec
sec:
	go run github.com/securego/gosec/v2/cmd/gosec@latest ./...

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

# Show help
help:
	@echo "Available commands:"
	@echo "  make              : Build the application after running format, lint, and test"
	@echo "  make build        : Build the application"
	@echo "  make clean        : Clean build files"
	@echo "  make test         : Run tests"
	@echo "  make lint         : Run linter"
	@echo "  make vet          : Run go vet"
	@echo "  make fmt          : Format code"
	@echo "  make run          : Build and run the server with dashboard enabled"
	@echo "  make docker       : Build docker image"
	@echo "  make docker-run   : Run docker container"
	@echo "  make install      : Install the application" 