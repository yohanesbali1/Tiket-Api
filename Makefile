BINARY_NAME := queue-api
BUILD_DIR := ./tmp
MAIN_PKG := ./cmd/server

.PHONY: all build run test lint clean docker-up docker-down docker-build

all: lint build

build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PKG)

run: build
	@echo "Running $(BINARY_NAME)..."
	@$(BUILD_DIR)/$(BINARY_NAME)

test:
	@echo "Running tests..."
	@go test -v -race ./...

lint:
	@echo "Running linter..."
	@golangci-lint run ./...

clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@go clean -cache

docker-build:
	@echo "Building Docker image..."
	@docker build -t ghcr.io/tiket-api:local .

docker-up:
	@echo "Starting services..."
	@IMAGE_REPO=tiket-api IMAGE_TAG=local docker compose up -d

docker-down:
	@echo "Stopping services..."
	@docker compose down

tidy:
	@go mod tidy

fmt:
	@gofmt -s -w .
