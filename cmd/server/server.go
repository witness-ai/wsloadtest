// Build with: go build -o server cmd/server/server.go
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Configuration flags
var (
	addr              = flag.String("addr", "0.0.0.0:8080", "http service address")
	customPayload     = flag.String("payload", "", "custom response payload")
	customRequest     = flag.String("request", "", "custom request payload")
	reqFile           = flag.String("req-file", "req.txt", "file containing request payload")
	respFile          = flag.String("resp-file", "resp.txt", "file containing response payload")
	maxConnections    = flag.Int("max-conns", 10000, "maximum number of concurrent connections")
	readBufferSize    = flag.Int("read-buffer", 8192, "read buffer size in bytes")
	writeBufferSize   = flag.Int("write-buffer", 8192, "write buffer size in bytes")
	readTimeout       = flag.Duration("read-timeout", 10*time.Second, "read timeout")
	writeTimeout      = flag.Duration("write-timeout", 10*time.Second, "write timeout")
	enableCompression = flag.Bool("compression", false, "enable websocket compression")
	enablePingPong    = flag.Bool("ping-pong", false, "enable ping/pong to keep connections alive")
	pingInterval      = flag.Duration("ping-interval", 30*time.Second, "ping interval when ping-pong is enabled")
)

var (
	requestPayload  []byte // Expected request payload
	responsePayload []byte // Response payload to send
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:    8192,
	WriteBufferSize:   8192,
	EnableCompression: false,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all connections
	},
}

// Metrics for monitoring
var (
	activeConnections  int32
	totalConnections   int64
	messagesSent       int64
	messagesReceived   int64
	connectionRejected int64
	errorCount         int64
	lastError          string
	connectionsMutex   sync.RWMutex
	startTime          time.Time
)

// readFileContent reads content from a file
func readFileContent(filename string) ([]byte, error) {
	// Try to read from current directory first
	content, err := os.ReadFile(filename)
	if err == nil {
		return content, nil
	}

	// Try to read from executable directory
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	exeDir := filepath.Dir(exePath)
	content, err = os.ReadFile(filepath.Join(exeDir, filename))
	if err != nil {
		return nil, err
	}

	return content, nil
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check if we've reached the connection limit
	if atomic.LoadInt32(&activeConnections) >= int32(*maxConnections) {
		atomic.AddInt64(&connectionRejected, 1)
		http.Error(w, "Too many connections", http.StatusServiceUnavailable)
		return
	}

	// Update upgrader with current configuration
	upgrader.ReadBufferSize = *readBufferSize
	upgrader.WriteBufferSize = *writeBufferSize
	upgrader.EnableCompression = *enableCompression

	// Upgrade the connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		atomic.AddInt64(&errorCount, 1)
		connectionsMutex.Lock()
		lastError = "upgrade error: " + err.Error()
		connectionsMutex.Unlock()
		log.Println("upgrade error:", err)
		return
	}
	defer conn.Close()

	// Set connection parameters
	conn.SetReadLimit(int64(*readBufferSize) * 2)
	if *readTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(*readTimeout))
	}
	if *writeTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(*writeTimeout))
	}

	// Update metrics
	atomic.AddInt32(&activeConnections, 1)
	atomic.AddInt64(&totalConnections, 1)
	defer atomic.AddInt32(&activeConnections, -1)

	log.Printf("Client connected: %s (active: %d, total: %d)",
		conn.RemoteAddr(), atomic.LoadInt32(&activeConnections), atomic.LoadInt64(&totalConnections))

	// Create a done channel for cleanup
	done := make(chan struct{})
	defer close(done)

	// Set up ping/pong if enabled
	if *enablePingPong {
		// Set up pong handler
		conn.SetPongHandler(func(string) error {
			if *readTimeout > 0 {
				conn.SetReadDeadline(time.Now().Add(*readTimeout))
			}
			return nil
		})

		// Start a goroutine to send pings periodically
		go func() {
			pingTicker := time.NewTicker(*pingInterval)
			defer pingTicker.Stop()
			for {
				select {
				case <-done:
					return
				case <-pingTicker.C:
					if *writeTimeout > 0 {
						conn.SetWriteDeadline(time.Now().Add(*writeTimeout))
					}
					if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
						return
					}
				}
			}
		}()
	}

	for {
		// Reset read deadline with each new message if timeout is enabled
		if *readTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(*readTimeout))
		}

		// Read message from client
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("read error: %v", err)
				atomic.AddInt64(&errorCount, 1)
				connectionsMutex.Lock()
				lastError = "read error: " + err.Error()
				connectionsMutex.Unlock()
			}
			break
		}
		atomic.AddInt64(&messagesReceived, 1)

		// Reset write deadline before sending if timeout is enabled
		if *writeTimeout > 0 {
			conn.SetWriteDeadline(time.Now().Add(*writeTimeout))
		}

		// Send payload to client
		err = conn.WriteMessage(websocket.TextMessage, responsePayload)
		if err != nil {
			log.Println("write error:", err)
			atomic.AddInt64(&errorCount, 1)
			connectionsMutex.Lock()
			lastError = "write error: " + err.Error()
			connectionsMutex.Unlock()
			break
		}
		atomic.AddInt64(&messagesSent, 1)
	}
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	uptime := time.Since(startTime).Seconds()

	// Get memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	connectionsMutex.RLock()
	lastErrorCopy := lastError
	connectionsMutex.RUnlock()

	status := struct {
		Uptime              float64 `json:"uptime_seconds"`
		ActiveConnections   int32   `json:"active_connections"`
		TotalConnections    int64   `json:"total_connections"`
		ConnectionsRejected int64   `json:"connections_rejected"`
		MessagesSent        int64   `json:"messages_sent"`
		MessagesReceived    int64   `json:"messages_received"`
		MessagesPerSecond   float64 `json:"messages_per_second"`
		ErrorCount          int64   `json:"error_count"`
		LastError           string  `json:"last_error,omitempty"`
		MaxConnections      int     `json:"max_connections"`
		PingPongEnabled     bool    `json:"ping_pong_enabled"`
		CompressionEnabled  bool    `json:"compression_enabled"`
		RequestPayloadSize  int     `json:"request_payload_size"`
		ResponsePayloadSize int     `json:"response_payload_size"`
		RequestFile         string  `json:"request_file"`
		ResponseFile        string  `json:"response_file"`
		NumGoroutines       int     `json:"num_goroutines"`
		MemoryAllocMB       float64 `json:"memory_alloc_mb"`
		SystemMemoryMB      float64 `json:"system_memory_mb"`
		NumCPU              int     `json:"num_cpu"`
		GoVersion           string  `json:"go_version"`
	}{
		Uptime:              uptime,
		ActiveConnections:   atomic.LoadInt32(&activeConnections),
		TotalConnections:    atomic.LoadInt64(&totalConnections),
		ConnectionsRejected: atomic.LoadInt64(&connectionRejected),
		MessagesSent:        atomic.LoadInt64(&messagesSent),
		MessagesReceived:    atomic.LoadInt64(&messagesReceived),
		MessagesPerSecond:   float64(atomic.LoadInt64(&messagesSent)) / uptime,
		ErrorCount:          atomic.LoadInt64(&errorCount),
		LastError:           lastErrorCopy,
		MaxConnections:      *maxConnections,
		PingPongEnabled:     *enablePingPong,
		CompressionEnabled:  *enableCompression,
		RequestPayloadSize:  len(requestPayload),
		ResponsePayloadSize: len(responsePayload),
		RequestFile:         *reqFile,
		ResponseFile:        *respFile,
		NumGoroutines:       runtime.NumGoroutine(),
		MemoryAllocMB:       float64(memStats.Alloc) / 1024 / 1024,
		SystemMemoryMB:      float64(memStats.Sys) / 1024 / 1024,
		NumCPU:              runtime.NumCPU(),
		GoVersion:           runtime.Version(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func main() {
	// Parse command line flags
	flag.Parse()

	// Set GOMAXPROCS to use all available CPUs
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Initialize start time
	startTime = time.Now()

	// Load request payload from file
	var err error
	requestPayload, err = readFileContent(*reqFile)
	if err != nil {
		log.Printf("Warning: Could not read request file %s: %v", *reqFile, err)
		requestPayload = []byte("default request")
	} else {
		log.Printf("Loaded request payload from %s (%d bytes)", *reqFile, len(requestPayload))
	}

	// Load response payload from file
	responsePayload, err = readFileContent(*respFile)
	if err != nil {
		log.Printf("Warning: Could not read response file %s: %v", *respFile, err)
		responsePayload = []byte("default response")
	} else {
		log.Printf("Loaded response payload from %s (%d bytes)", *respFile, len(responsePayload))
	}

	// Override with custom payloads if provided
	if *customRequest != "" {
		requestPayload = []byte(*customRequest)
		log.Printf("Using custom request payload (%d bytes)", len(requestPayload))
	}

	if *customPayload != "" {
		responsePayload = []byte(*customPayload)
		log.Printf("Using custom response payload (%d bytes)", len(responsePayload))
	}

	// Configure server timeouts
	server := &http.Server{
		Addr:              *addr,
		ReadHeaderTimeout: 3 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Set up routes
	http.HandleFunc("/", handleWebSocket)
	http.HandleFunc("/status", handleStatus)

	// Print startup information
	log.Printf("WebSocket server starting on %s", *addr)
	log.Printf("WebSocket endpoint: ws://%s/", *addr)
	log.Printf("Status endpoint: http://%s/status", *addr)
	log.Printf("Max connections: %d", *maxConnections)
	log.Printf("Ping/Pong enabled: %v", *enablePingPong)
	log.Printf("Compression enabled: %v", *enableCompression)
	log.Printf("Running with %d CPUs", runtime.NumCPU())

	// Start the server
	log.Fatal(server.ListenAndServe())
}
