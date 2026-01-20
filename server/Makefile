.PHONY: build test test-integration run clean

# Build the server binary
build:
	go build -o server ./cmd/server

# Run unit tests
test:
	go test -v ./internal/...

# Run integration tests (requires Claude CLI)
test-integration:
	go test -tags=integration -v -timeout=5m

# Run the server
run: build
	./server

# Run with custom options
run-dev: build
	./server -port 8080 -db chai-dev.db

# Clean build artifacts
clean:
	rm -f server chai*.db

# Show help
help:
	@echo "Available targets:"
	@echo "  build           - Build the server binary"
	@echo "  test            - Run unit tests"
	@echo "  test-integration - Run integration tests (requires Claude CLI)"
	@echo "  run             - Build and run the server"
	@echo "  run-dev         - Run in development mode"
	@echo "  clean           - Remove build artifacts"
