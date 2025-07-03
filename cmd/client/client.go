// Build with: go build -o client cmd/client/client.go
package main

import (
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var (
	addr               = flag.String("addr", "localhost:8080", "server address")
	connections        = flag.Int("connections", 10, "number of concurrent connections")
	messageRate        = flag.Int("rate", 10, "messages per second per connection")
	testDuration       = flag.Int("duration", 60, "test duration in seconds")
	batchSize          = flag.Int("batch", 100, "connection batch size")
	batchDelay         = flag.Duration("batch-delay", 1*time.Second, "delay between connection batches")
	readBufferSize     = flag.Int("read-buffer", 8192, "read buffer size")
	writeBufferSize    = flag.Int("write-buffer", 8192, "write buffer size")
	readTimeout        = flag.Duration("read-timeout", 10*time.Second, "read timeout")
	writeTimeout       = flag.Duration("write-timeout", 10*time.Second, "write timeout")
	enableCompression  = flag.Bool("compression", false, "enable compression")
	verbose            = flag.Bool("verbose", false, "verbose logging of messages")
	reqFile            = flag.String("req-file", "req.txt", "file containing request payload")
	customRequest      = flag.String("request", "", "custom request payload")
	messagesSent       int64
	messagesRecv       int64
	activeConns        int32
	totalErrors        int64
	connectionFailures int64
	testStartTime      time.Time
	wg                 sync.WaitGroup
	stopChan           chan struct{}
	requestPayload     []byte // Request payload to send
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

func runClient(id int) {
	defer wg.Done()

	// Prepare dialer with options
	dialer := websocket.Dialer{
		ReadBufferSize:    *readBufferSize,
		WriteBufferSize:   *writeBufferSize,
		HandshakeTimeout:  10 * time.Second,
		EnableCompression: *enableCompression,
		Proxy:             http.ProxyFromEnvironment,
	}

	u := url.URL{Scheme: "ws", Host: *addr, Path: "/"}
	log.Printf("Client %d connecting to %s", id, u.String())

	// Connect with retry logic
	var conn *websocket.Conn
	var resp *http.Response
	var err error

	maxRetries := 3
	for retry := 0; retry < maxRetries; retry++ {
		conn, resp, err = dialer.Dial(u.String(), nil)
		if err == nil {
			break
		}

		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
		}

		if retry < maxRetries-1 {
			log.Printf("Client %d dial error (attempt %d/%d): %v (status: %d), retrying in 1s",
				id, retry+1, maxRetries, err, statusCode)
			select {
			case <-time.After(1 * time.Second):
				continue
			case <-stopChan:
				return
			}
		} else {
			log.Printf("Client %d dial error: %v (status: %d), giving up", id, err, statusCode)
			atomic.AddInt64(&connectionFailures, 1)
			atomic.AddInt64(&totalErrors, 1)
			return
		}
	}

	defer conn.Close()

	atomic.AddInt32(&activeConns, 1)
	defer atomic.AddInt32(&activeConns, -1)

	// Create a ticker for sending messages at the specified rate
	interval := time.Second / time.Duration(*messageRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Set up a channel to handle interruption
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Calculate end time
	endTime := testStartTime.Add(time.Duration(*testDuration) * time.Second)

	done := make(chan struct{})

	// Start a goroutine to read messages from the server
	go func() {
		defer close(done)
		for {
			// Set read deadline
			if *readTimeout > 0 {
				conn.SetReadDeadline(time.Now().Add(*readTimeout))
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				select {
				case <-stopChan:
					// Test is stopping, no need to log the error
					return
				default:
					log.Printf("Client %d read error: %v", id, err)
					atomic.AddInt64(&totalErrors, 1)
					return
				}
			}
			atomic.AddInt64(&messagesRecv, 1)

			if *verbose {
				if len(message) > 100 {
					log.Printf("Client %d received: %s... (%d bytes)", id, message[:100], len(message))
				} else {
					log.Printf("Client %d received: %s", id, message)
				}
			}
		}
	}()

	// Send messages at the specified rate until the test duration is reached
	for {
		select {
		case <-done:
			return
		case <-stopChan:
			return
		case <-ticker.C:
			if time.Now().After(endTime) {
				return
			}

			// Set write deadline
			if *writeTimeout > 0 {
				conn.SetWriteDeadline(time.Now().Add(*writeTimeout))
			}

			// Send the request payload instead of a generic message
			err := conn.WriteMessage(websocket.TextMessage, requestPayload)
			if err != nil {
				log.Printf("Client %d write error: %v", id, err)
				atomic.AddInt64(&totalErrors, 1)
				return
			}
			atomic.AddInt64(&messagesSent, 1)
		case <-interrupt:
			log.Println("Interrupt received, closing connection")

			// Set write deadline for close message
			if *writeTimeout > 0 {
				conn.SetWriteDeadline(time.Now().Add(*writeTimeout))
			}

			err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Printf("Client %d close error: %v", id, err)
				atomic.AddInt64(&totalErrors, 1)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}

func printStats() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			elapsed := time.Since(testStartTime).Seconds()
			if elapsed > float64(*testDuration) {
				return
			}

			// Get memory stats
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)

			log.Printf("Stats: Active=%d, Sent=%d, Received=%d, Errors=%d, ConnErrs=%d, Rate=%.2f/s, Mem=%.1fMB",
				atomic.LoadInt32(&activeConns),
				atomic.LoadInt64(&messagesSent),
				atomic.LoadInt64(&messagesRecv),
				atomic.LoadInt64(&totalErrors),
				atomic.LoadInt64(&connectionFailures),
				float64(atomic.LoadInt64(&messagesSent))/elapsed,
				float64(memStats.Alloc)/1024/1024)
		case <-stopChan:
			return
		}
	}
}

func main() {
	// Parse flags
	flag.Parse()

	// Set GOMAXPROCS to use all available CPUs
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Initialize channels
	stopChan = make(chan struct{})

	// Initialize start time
	testStartTime = time.Now()

	// Load request payload from file
	var err error
	requestPayload, err = readFileContent(*reqFile)
	if err != nil {
		log.Printf("Warning: Could not read request file %s: %v", *reqFile, err)
		requestPayload = []byte("default request")
	} else {
		log.Printf("Loaded request payload from %s (%d bytes)", *reqFile, len(requestPayload))
	}

	// Override with custom payload if provided
	if *customRequest != "" {
		requestPayload = []byte(*customRequest)
		log.Printf("Using custom request payload (%d bytes)", len(requestPayload))
	}

	// Set up signal handling for graceful shutdown
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Start the stats printer
	go printStats()

	// Start the clients in batches to avoid overwhelming the server
	log.Printf("Starting %d connections in batches of %d with %s delay between batches",
		*connections, *batchSize, *batchDelay)

	for i := 0; i < *connections; {
		// Check for interrupt signal
		select {
		case <-interrupt:
			log.Println("Interrupt received, stopping test")
			close(stopChan)
			wg.Wait()
			printFinalStats()
			return
		default:
			// Continue with the test
		}

		// Calculate batch size
		batchEnd := i + *batchSize
		if batchEnd > *connections {
			batchEnd = *connections
		}

		// Start a batch of clients
		log.Printf("Starting connections %d to %d...", i+1, batchEnd)
		for j := i; j < batchEnd; j++ {
			wg.Add(1)
			go runClient(j + 1)
		}

		i = batchEnd

		// If we have more connections to create, wait before the next batch
		if i < *connections {
			select {
			case <-time.After(*batchDelay):
				// Continue with the next batch
			case <-interrupt:
				log.Println("Interrupt received, stopping test")
				close(stopChan)
				break
			}
		}
	}

	// Wait for test duration or interrupt
	select {
	case <-time.After(time.Duration(*testDuration) * time.Second):
		log.Printf("Test duration completed (%d seconds)", *testDuration)
	case <-interrupt:
		log.Println("Interrupt received, stopping test")
	}

	// Signal all clients to stop
	close(stopChan)

	// Wait for all clients to complete
	log.Println("Waiting for all clients to disconnect...")
	wg.Wait()

	// Print final stats
	printFinalStats()
}

func printFinalStats() {
	elapsed := time.Since(testStartTime).Seconds()

	// Get memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	log.Printf("Test completed in %.2f seconds", elapsed)
	log.Printf("Final stats:")
	log.Printf("  Connections:       %d", *connections)
	log.Printf("  Messages sent:     %d", atomic.LoadInt64(&messagesSent))
	log.Printf("  Messages received: %d", atomic.LoadInt64(&messagesRecv))
	log.Printf("  Message rate:      %.2f messages/second", float64(atomic.LoadInt64(&messagesSent))/elapsed)
	log.Printf("  Active conns:      %d", atomic.LoadInt32(&activeConns))
	log.Printf("  Total errors:      %d", atomic.LoadInt64(&totalErrors))
	log.Printf("  Connection errors: %d", atomic.LoadInt64(&connectionFailures))
	log.Printf("  Memory usage:      %.2f MB", float64(memStats.Alloc)/1024/1024)
	log.Printf("  System memory:     %.2f MB", float64(memStats.Sys)/1024/1024)
	log.Printf("  NumGoroutine:      %d", runtime.NumGoroutine())
}
