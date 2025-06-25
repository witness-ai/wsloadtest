# WebSocket Load Testing Server

A simple WebSocket server written in Go for load testing proxies, optimized to handle thousands of concurrent connections.

## Features

- Accepts WebSocket connections
- Responds to any message with a configurable payload
- Allows custom address/port binding
- Allows all cross-origin requests for easy testing
- Provides real-time metrics via status endpoint
- Includes a load testing client
- Optimized for high-performance with thousands of concurrent connections
- Configurable connection limits, timeouts, and buffer sizes
- Optional ping/pong mechanism for long-lived connections
- Loads request and response payloads from files
- Optional request validation

## Project Structure

```
wsloadtest/
├── cmd/
│   ├── server/     # WebSocket server implementation
│   └── client/     # Load testing client implementation
├── req.txt         # Default request payload
├── resp.txt        # Default response payload
├── run_test.sh     # Automated test script
└── README.md
```

## Usage

### Build the server

```bash
go build -o server cmd/server/server.go
```

### Run the server

```bash
# Run with default settings (0.0.0.0:8080)
./server

# Run with custom port
./server -addr=0.0.0.0:9000

# Run with custom payload
./server -payload="custom response payload"

# Run with custom request and response payloads
./server -request="custom request" -payload="custom response"

# Specify custom payload files
./server -req-file=custom-req.txt -resp-file=custom-resp.txt

# Enable request validation (checks if client requests match expected request)
./server -validate-request=true

# Run with increased connection limit (default is 10000)
./server -max-conns=20000

# Run with custom buffer sizes and timeouts
./server -read-buffer=4096 -write-buffer=4096 -read-timeout=20s -write-timeout=20s

# Enable ping/pong for long-lived connections
./server -ping-pong=true -ping-interval=30s
```

### Server Configuration Options

The server supports several configuration options to optimize performance:

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | 0.0.0.0:8080 | HTTP service address |
| `-payload` | "" | Custom response payload (overrides file) |
| `-request` | "" | Custom request payload (overrides file) |
| `-req-file` | req.txt | File containing request payload |
| `-resp-file` | resp.txt | File containing response payload |
| `-validate-request` | false | Validate that client requests match expected request |
| `-max-conns` | 10000 | Maximum number of concurrent connections |
| `-read-buffer` | 1024 | Read buffer size in bytes |
| `-write-buffer` | 1024 | Write buffer size in bytes |
| `-read-timeout` | 10s | Read timeout |
| `-write-timeout` | 10s | Write timeout |
| `-idle-timeout` | 60s | Idle timeout |
| `-compression` | true | Enable WebSocket compression |
| `-ping-pong` | false | Enable ping/pong to keep connections alive |
| `-ping-interval` | 30s | Ping interval when ping-pong is enabled |

### System Tuning for High Performance

To handle thousands of connections, you may need to increase system limits:

```bash
# Increase maximum number of open files (as root)
sysctl -w kern.maxfiles=500000
sysctl -w kern.maxfilesperproc=250000
ulimit -n 250000
```

### Connect to the server

The WebSocket endpoint is available at:
```
ws://[server-address]:8080/
```

### Monitor server metrics

The server provides a status endpoint with real-time metrics:
```
http://[server-address]:8080/status
```

This returns a JSON object with the following metrics:
```json
{
  "uptime_seconds": 123.45,
  "active_connections": 42,
  "total_connections": 100,
  "connections_rejected": 0,
  "messages_sent": 500,
  "messages_received": 500,
  "messages_per_second": 4.05,
  "error_count": 0,
  "last_error": "",
  "max_connections": 10000,
  "ping_pong_enabled": false,
  "validate_request": false,
  "request_payload_size": 1234,
  "response_payload_size": 4321,
  "request_file": "req.txt",
  "response_file": "resp.txt",
  "num_goroutines": 46,
  "memory_alloc_mb": 8.45,
  "system_memory_mb": 16.78,
  "num_cpu": 8,
  "go_version": "go1.18.1"
}
```

When request validation is enabled, the status endpoint also includes:
```json
{
  "requests_validated": 450,
  "requests_invalid": 50
}
```

## Load Testing

### Using the automated test script

The easiest way to run a complete load test is using the included script:

```bash
# Run with default settings (100 connections, 10 msg/s per connection, 60 seconds)
./run_test.sh

# Run with custom settings
./run_test.sh -c 500 -r 20 -d 300 -p 9000

# High-load test with 5000 connections
./run_test.sh -c 5000 -r 5 -d 300 -b 100 -w 2s
```

The script supports the following options:
- `-p, --port`: Server port (default: 8080)
- `-c, --connections`: Number of client connections (default: 100)  
- `-r, --rate`: Messages per second per connection (default: 10)
- `-d, --duration`: Test duration in seconds (default: 60)
- `-m, --max-conns`: Maximum server connections (default: 10000)
- `-b, --batch-size`: Connection batch size (default: 20)
- `-w, --batch-delay`: Delay between batches (default: 1s)
- `-s, --buffer-size`: Buffer size in bytes (default: 8192)
- `-q, --req-file`: Request payload file (default: req.txt)
- `-e, --resp-file`: Response payload file (default: resp.txt)

### Using the included Go client

The repository includes a WebSocket client specifically designed for load testing:

```bash
# Build the client
go build -o client cmd/client/client.go

# Run the client with default settings (10 connections, 10 msgs/sec per connection, 60 seconds)
./client

# Run with custom settings
./client -addr=localhost:8080 -connections=100 -rate=50 -duration=300

# For high-load testing with thousands of connections
./client -addr=localhost:8080 -connections=5000 -rate=5 -duration=300

# For high-load testing with connection batching (to avoid overwhelming the server)
./client -connections=5000 -batch=500 -batch-delay=5s
```

### Client Configuration Options

The client supports several configuration options to optimize load testing:

| Flag | Default | Description |
|------|---------|-------------|
| `-addr` | localhost:8080 | Server address |
| `-connections` | 10 | Number of concurrent connections |
| `-rate` | 10 | Messages per second per connection |
| `-duration` | 60 | Test duration in seconds |
| `-batch` | 100 | Connection batch size |
| `-batch-delay` | 1s | Delay between connection batches |
| `-read-buffer` | 1024 | Read buffer size |
| `-write-buffer` | 1024 | Write buffer size |
| `-read-timeout` | 10s | Read timeout |
| `-write-timeout` | 10s | Write timeout |
| `-compression` | true | Enable WebSocket compression |
| `-verbose` | false | Enable verbose message logging |

### Using other tools

For load testing the status endpoint, you can use tools like [hey](https://github.com/rakyll/hey):

```bash
hey -n 1000 -c 100 http://localhost:8080/status
```

## Client Example

You can use any WebSocket client to connect to this server. For example, with JavaScript:

```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onopen = () => {
  console.log('Connected to server');
  ws.send('Any message will trigger a response');
};

ws.onmessage = (event) => {
  console.log('Received:', event.data);
};

ws.onclose = () => {
  console.log('Connection closed');
};
``` 