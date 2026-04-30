package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// SSEBroker manages Server-Sent Events subscriptions.
type SSEBroker struct {
	mu      sync.RWMutex
	clients map[chan string]struct{}
}

// NewSSEBroker creates a new SSE broker.
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{clients: make(map[chan string]struct{})}
}

// Publish sends an event to all connected SSE clients.
func (b *SSEBroker) Publish(event string, data any) {
	// Recover from json.Marshal panic (e.g., cyclic data structures)
	var payload []byte
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				payload = []byte(`{"error":"marshal panic"}`)
			}
		}()
		payload, _ = json.Marshal(data)
	}()
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, payload)
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- msg:
		default: // drop if slow consumer
		}
	}
}

// ServeHTTP handles SSE connections at GET /yaad/events.
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan string, 32)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
	}()

	// Send initial ping
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case msg := <-ch:
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
