#!/bin/bash
# Script to run WebSocket load test with both server and client

# Default configuration
SERVER_PORT=8080
CLIENT_CONNECTIONS=100
CLIENT_RATE=10
TEST_DURATION=60
SERVER_MAX_CONNS=10000
BATCH_SIZE=20
BATCH_DELAY="1s"
BUFFER_SIZE=8192
REQ_FILE="req.txt"
RESP_FILE="resp.txt"
SERVER_BINARY="./server"
CLIENT_BINARY="./client"

# Function to display usage
show_usage() {
  echo "Usage: $0 [options]"
  echo "Options:"
  echo "  -p, --port PORT            Server port (default: $SERVER_PORT)"
  echo "  -c, --connections NUM      Number of client connections (default: $CLIENT_CONNECTIONS)"
  echo "  -r, --rate NUM             Messages per second per connection (default: $CLIENT_RATE)"
  echo "  -d, --duration SECONDS     Test duration in seconds (default: $TEST_DURATION)"
  echo "  -m, --max-conns NUM        Maximum server connections (default: $SERVER_MAX_CONNS)"
  echo "  -b, --batch-size NUM       Connection batch size (default: $BATCH_SIZE)"
  echo "  -w, --batch-delay DURATION Delay between batches (default: $BATCH_DELAY)"
  echo "  -s, --buffer-size NUM      Buffer size in bytes (default: $BUFFER_SIZE)"
  echo "  -q, --req-file FILE        Request payload file (default: $REQ_FILE)"
  echo "  -e, --resp-file FILE       Response payload file (default: $RESP_FILE)"
  echo "  -h, --help                 Show this help message"
  exit 1
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    -p|--port)
      SERVER_PORT="$2"
      shift 2
      ;;
    -c|--connections)
      CLIENT_CONNECTIONS="$2"
      shift 2
      ;;
    -r|--rate)
      CLIENT_RATE="$2"
      shift 2
      ;;
    -d|--duration)
      TEST_DURATION="$2"
      shift 2
      ;;
    -m|--max-conns)
      SERVER_MAX_CONNS="$2"
      shift 2
      ;;
    -b|--batch-size)
      BATCH_SIZE="$2"
      shift 2
      ;;
    -w|--batch-delay)
      BATCH_DELAY="$2"
      shift 2
      ;;
    -s|--buffer-size)
      BUFFER_SIZE="$2"
      shift 2
      ;;
    -q|--req-file)
      REQ_FILE="$2"
      shift 2
      ;;
    -e|--resp-file)
      RESP_FILE="$2"
      shift 2
      ;;
    -h|--help)
      show_usage
      ;;
    *)
      echo "Unknown option: $1"
      show_usage
      ;;
  esac
done

# Clean up any existing binaries
rm -f $SERVER_BINARY $CLIENT_BINARY

# Build the server and client
echo "Building server and client..."
go build -o $SERVER_BINARY cmd/server/server.go
if [ $? -ne 0 ]; then
  echo "Server build failed. Exiting."
  exit 1
fi

go build -o $CLIENT_BINARY cmd/client/client.go
if [ $? -ne 0 ]; then
  echo "Client build failed. Exiting."
  exit 1
fi

echo "Build successful!"

# Start the server
echo "Starting WebSocket server on port $SERVER_PORT with max connections: $SERVER_MAX_CONNS"
SERVER_CMD="$SERVER_BINARY -addr=0.0.0.0:$SERVER_PORT -max-conns=$SERVER_MAX_CONNS -read-buffer=$BUFFER_SIZE -write-buffer=$BUFFER_SIZE -req-file=$REQ_FILE -resp-file=$RESP_FILE"

# Start server in background
$SERVER_CMD &
SERVER_PID=$!

# Wait for server to start
echo "Waiting for server to start..."
sleep 2

# Check if server is running
if ! ps -p $SERVER_PID > /dev/null; then
  echo "Server failed to start. Exiting."
  exit 1
fi

echo "Server started with PID: $SERVER_PID"

# Start the client
echo "Starting WebSocket client with $CLIENT_CONNECTIONS connections, $CLIENT_RATE msg/s per connection, $TEST_DURATION seconds duration"
CLIENT_CMD="$CLIENT_BINARY -addr=localhost:$SERVER_PORT -connections=$CLIENT_CONNECTIONS -rate=$CLIENT_RATE -duration=$TEST_DURATION -batch=$BATCH_SIZE -batch-delay=$BATCH_DELAY -read-buffer=$BUFFER_SIZE -write-buffer=$BUFFER_SIZE -req-file=$REQ_FILE"

echo "Running: $CLIENT_CMD"
$CLIENT_CMD

# After client finishes, kill the server
echo "Test completed. Shutting down server..."
kill $SERVER_PID

echo "Done!" 