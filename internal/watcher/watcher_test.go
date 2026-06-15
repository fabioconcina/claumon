package watcher

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClassifyAndDispatch(t *testing.T) {
	var sessionPath, memoryPath string

	w := &Watcher{
		onSessionChange: func(path string) { sessionPath = path },
		onMemoryChange:  func(path string) { memoryPath = path },
	}

	w.classifyAndDispatch("/some/path/session.jsonl")
	if sessionPath != "/some/path/session.jsonl" {
		t.Errorf("session callback not called, got %q", sessionPath)
	}
	if memoryPath != "" {
		t.Errorf("memory callback should not be called for .jsonl, got %q", memoryPath)
	}

	sessionPath = ""
	w.classifyAndDispatch("/some/path/CLAUDE.md")
	if memoryPath != "/some/path/CLAUDE.md" {
		t.Errorf("memory callback not called, got %q", memoryPath)
	}
	if sessionPath != "" {
		t.Errorf("session callback should not be called for .md, got %q", sessionPath)
	}
}

func TestClassifyAndDispatchIgnoresOtherFiles(t *testing.T) {
	called := false
	w := &Watcher{
		onSessionChange: func(path string) { called = true },
		onMemoryChange:  func(path string) { called = true },
	}

	w.classifyAndDispatch("/some/path/file.txt")
	if called {
		t.Error("neither callback should be called for .txt files")
	}

	w.classifyAndDispatch("/some/path/file.go")
	if called {
		t.Error("neither callback should be called for .go files")
	}
}

func TestClassifyNilCallbacks(t *testing.T) {
	w := &Watcher{}

	// Should not panic with nil callbacks
	w.classifyAndDispatch("/path/session.jsonl")
	w.classifyAndDispatch("/path/note.md")
}

// A burst of triggers for one key should collapse into a single fire once the
// key goes idle.
func TestDebouncerCoalescesBurst(t *testing.T) {
	var fires int32
	var got []string
	var mu sync.Mutex
	deb := newDebouncer(40*time.Millisecond, time.Second, func(key string) {
		atomic.AddInt32(&fires, 1)
		mu.Lock()
		got = append(got, key)
		mu.Unlock()
	})
	defer deb.stop()

	// 10 triggers spaced well under idleDelay: should fire exactly once.
	for i := 0; i < 10; i++ {
		deb.trigger("/path/a.jsonl")
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(120 * time.Millisecond)

	if n := atomic.LoadInt32(&fires); n != 1 {
		t.Fatalf("expected 1 fire from coalesced burst, got %d", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != "/path/a.jsonl" {
		t.Fatalf("unexpected fired keys: %v", got)
	}
}

// A key that never goes idle must still fire within maxWait rather than being
// starved forever.
func TestDebouncerMaxWaitCap(t *testing.T) {
	var fires int32
	deb := newDebouncer(50*time.Millisecond, 120*time.Millisecond, func(string) {
		atomic.AddInt32(&fires, 1)
	})
	defer deb.stop()

	// Hammer the key for longer than maxWait, always re-arming before idleDelay
	// elapses. Without the cap this would never fire.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		deb.trigger("/path/hot.jsonl")
		time.Sleep(20 * time.Millisecond)
	}
	if n := atomic.LoadInt32(&fires); n < 1 {
		t.Fatalf("expected at least one fire under sustained load, got %d", n)
	}
}

// Distinct keys are debounced independently.
func TestDebouncerSeparateKeys(t *testing.T) {
	var fires int32
	deb := newDebouncer(30*time.Millisecond, time.Second, func(string) {
		atomic.AddInt32(&fires, 1)
	})
	defer deb.stop()

	deb.trigger("/a.jsonl")
	deb.trigger("/b.jsonl")
	time.Sleep(100 * time.Millisecond)

	if n := atomic.LoadInt32(&fires); n != 2 {
		t.Fatalf("expected 2 fires for 2 distinct keys, got %d", n)
	}
}
