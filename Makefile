.PHONY: all build clean server client test

# Default target
all: build

# Build both server and client
build: server client

# Build server
server:
	go build -o server cmd/server/server.go

# Build client  
client:
	go build -o client cmd/client/client.go

# Clean built binaries
clean:
	rm -f server client

# Run tests (builds and runs a quick test)
test: build
	./run_test.sh -c 10 -r 5 -d 10

# Install dependencies
deps:
	go mod download
	go mod tidy

# Run server with default settings
run-server: server
	./server

# Run client with default settings (requires server to be running)
run-client: client
	./client

# Help target
help:
	@echo "Available targets:"
	@echo "  all        - Build both server and client (default)"
	@echo "  build      - Build both server and client"
	@echo "  server     - Build server only"
	@echo "  client     - Build client only"
	@echo "  clean      - Remove built binaries"
	@echo "  test       - Build and run a quick test"
	@echo "  deps       - Download and tidy dependencies"
	@echo "  run-server - Build and run server"
	@echo "  run-client - Build and run client"
	@echo "  help       - Show this help" 