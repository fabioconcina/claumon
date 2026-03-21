package watcher

import (
	"testing"
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
