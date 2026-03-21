package server

import (
	"context"
	"testing"
	"time"
)

func TestBrokerSend(t *testing.T) {
	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broker.Run(ctx)

	// Register a client
	ch := make(chan SSEEvent, 32)
	broker.register <- ch

	// Give the broker time to register
	time.Sleep(10 * time.Millisecond)

	broker.Send(SSEEvent{Event: "test", Data: `{"msg":"hello"}`})

	select {
	case evt := <-ch:
		if evt.Event != "test" {
			t.Errorf("event = %q, want %q", evt.Event, "test")
		}
		if evt.Data != `{"msg":"hello"}` {
			t.Errorf("data = %q, want %q", evt.Data, `{"msg":"hello"}`)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for SSE event")
	}

	// Unregister
	broker.unregister <- ch
	time.Sleep(10 * time.Millisecond)
}

func TestBrokerContextCancellation(t *testing.T) {
	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		broker.Run(ctx)
		close(done)
	}()

	ch := make(chan SSEEvent, 32)
	broker.register <- ch
	time.Sleep(10 * time.Millisecond)

	cancel()

	select {
	case <-done:
		// OK - broker exited
	case <-time.After(time.Second):
		t.Fatal("broker did not exit after context cancellation")
	}
}

func TestBrokerBroadcastToMultipleClients(t *testing.T) {
	broker := NewBroker()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go broker.Run(ctx)

	ch1 := make(chan SSEEvent, 32)
	ch2 := make(chan SSEEvent, 32)
	broker.register <- ch1
	broker.register <- ch2
	time.Sleep(10 * time.Millisecond)

	broker.Send(SSEEvent{Event: "update", Data: "data"})

	for _, ch := range []chan SSEEvent{ch1, ch2} {
		select {
		case evt := <-ch:
			if evt.Event != "update" {
				t.Errorf("event = %q, want %q", evt.Event, "update")
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}
	}
}
