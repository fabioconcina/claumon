package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type SSEEvent struct {
	Event string
	Data  string
}

type SSEBroker struct {
	mu         sync.RWMutex
	clients    map[chan SSEEvent]bool
	broadcast  chan SSEEvent
	register   chan chan SSEEvent
	unregister chan chan SSEEvent
}

func NewBroker() *SSEBroker {
	return &SSEBroker{
		clients:    make(map[chan SSEEvent]bool),
		broadcast:  make(chan SSEEvent, 64),
		register:   make(chan chan SSEEvent),
		unregister: make(chan chan SSEEvent),
	}
}

func (b *SSEBroker) Run(ctx context.Context) {
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.mu.Lock()
			for ch := range b.clients {
				close(ch)
				delete(b.clients, ch)
			}
			b.mu.Unlock()
			return

		case ch := <-b.register:
			b.mu.Lock()
			b.clients[ch] = true
			b.mu.Unlock()
			log.Printf("[sse] client connected (%d total)", len(b.clients))

		case ch := <-b.unregister:
			b.mu.Lock()
			if _, ok := b.clients[ch]; ok {
				close(ch)
				delete(b.clients, ch)
			}
			b.mu.Unlock()
			log.Printf("[sse] client disconnected (%d total)", len(b.clients))

		case event := <-b.broadcast:
			b.mu.RLock()
			for ch := range b.clients {
				select {
				case ch <- event:
				default:
					// Client too slow, skip
				}
			}
			b.mu.RUnlock()

		case <-pingTicker.C:
			b.mu.RLock()
			for ch := range b.clients {
				select {
				case ch <- SSEEvent{Event: "ping", Data: `{"time":"` + time.Now().Format(time.RFC3339) + `"}`}:
				default:
				}
			}
			b.mu.RUnlock()
		}
	}
}

func (b *SSEBroker) Send(event SSEEvent) {
	select {
	case b.broadcast <- event:
	default:
		log.Printf("[sse] broadcast channel full, dropping event: %s", event.Event)
	}
}

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

	ch := make(chan SSEEvent, 32)
	b.register <- ch

	defer func() {
		b.unregister <- ch
	}()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Event, event.Data)
			flusher.Flush()
		}
	}
}
