package watcher

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	fsw             *fsnotify.Watcher
	claudeDir       string
	onSessionChange func(path string)
	onMemoryChange  func(path string)
}

func New(claudeDir string) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", err)
	}

	w := &Watcher{
		fsw:       fsw,
		claudeDir: claudeDir,
	}

	// Watch the claude dir itself (for CLAUDE.md changes)
	fsw.Add(claudeDir)

	// Watch rules dir if it exists
	rulesDir := filepath.Join(claudeDir, "rules")
	if _, err := os.Stat(rulesDir); err == nil {
		fsw.Add(rulesDir)
	}

	// Watch all project directories and their memory subdirs
	projectsDir := filepath.Join(claudeDir, "projects")
	if entries, err := os.ReadDir(projectsDir); err == nil {
		fsw.Add(projectsDir)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			projDir := filepath.Join(projectsDir, e.Name())
			fsw.Add(projDir)

			memDir := filepath.Join(projDir, "memory")
			if _, err := os.Stat(memDir); err == nil {
				fsw.Add(memDir)
			}
		}
	}

	return w, nil
}

func (w *Watcher) OnSessionChange(fn func(path string)) {
	w.onSessionChange = fn
}

func (w *Watcher) OnMemoryChange(fn func(path string)) {
	w.onMemoryChange = fn
}

func (w *Watcher) Start(ctx context.Context) error {
	// Trailing-edge debounce. A file that is actively written (e.g. a live
	// session JSONL) emits Write events many times per second; dispatching each
	// one re-parses every session file, pegging a core. Instead we coalesce
	// per-path bursts into a single dispatch once writes go idle, capped by
	// maxWait so a continuously-written file still refreshes periodically.
	const (
		idleDelay = 750 * time.Millisecond
		maxWait   = 3 * time.Second
	)

	// Fired paths are funnelled back through the select loop so dispatch stays
	// single-threaded, matching the original (serialized) behaviour.
	fire := make(chan string, 64)
	deb := newDebouncer(idleDelay, maxWait, func(path string) {
		select {
		case fire <- path:
		case <-ctx.Done():
		}
	})
	defer deb.stop()

	for {
		select {
		case <-ctx.Done():
			return w.fsw.Close()
		case event, ok := <-w.fsw.Events:
			if !ok {
				return nil
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			// If a new directory is created under projects, start watching it.
			// This is handled immediately (not debounced) so we register the
			// watch before more files land inside it.
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					w.fsw.Add(event.Name)
					// Also watch memory subdir if created
					memDir := filepath.Join(event.Name, "memory")
					if _, err := os.Stat(memDir); err == nil {
						w.fsw.Add(memDir)
					}
					// Scan for session files that arrived before watch was registered
					if dirEntries, err := os.ReadDir(event.Name); err == nil {
						for _, de := range dirEntries {
							if !de.IsDir() {
								deb.trigger(filepath.Join(event.Name, de.Name()))
							}
						}
					}
					continue
				}
			}

			deb.trigger(event.Name)

		case path := <-fire:
			w.classifyAndDispatch(path)

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return nil
			}
			log.Printf("[watcher] error: %v", err)
		}
	}
}

// debouncer coalesces rapid per-key triggers into a single trailing-edge fire.
// A key that keeps receiving triggers fires once it stays idle for idleDelay,
// or at the latest maxWait after its first trigger so a continuously-active key
// is not starved. fn runs on a timer goroutine; keep it cheap (here it just
// hands the key back to the watcher loop).
type debouncer struct {
	idleDelay time.Duration
	maxWait   time.Duration
	fn        func(string)

	mu     sync.Mutex
	timers map[string]*debounceEntry
}

type debounceEntry struct {
	timer     *time.Timer
	firstSeen time.Time
}

func newDebouncer(idle, max time.Duration, fn func(string)) *debouncer {
	return &debouncer{
		idleDelay: idle,
		maxWait:   max,
		fn:        fn,
		timers:    make(map[string]*debounceEntry),
	}
}

// trigger records activity for key and (re)arms its trailing-edge timer.
func (d *debouncer) trigger(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	e, ok := d.timers[key]
	if !ok {
		e = &debounceEntry{firstSeen: now}
		d.timers[key] = e
	} else if e.timer != nil {
		e.timer.Stop()
	}

	delay := d.idleDelay
	if rem := d.maxWait - now.Sub(e.firstSeen); rem < delay {
		if rem < 0 {
			rem = 0
		}
		delay = rem
	}

	e.timer = time.AfterFunc(delay, func() {
		d.mu.Lock()
		// Only fire if this entry is still the current one for key; a trigger
		// that arrived after this timer was scheduled will have replaced it.
		if cur, ok := d.timers[key]; ok && cur == e {
			delete(d.timers, key)
		}
		d.mu.Unlock()
		d.fn(key)
	})
}

// stop cancels all pending timers. Already-fired timers may still deliver.
func (d *debouncer) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, e := range d.timers {
		if e.timer != nil {
			e.timer.Stop()
		}
	}
	d.timers = make(map[string]*debounceEntry)
}

func (w *Watcher) classifyAndDispatch(path string) {
	// Session JSONL files
	if strings.HasSuffix(path, ".jsonl") {
		if w.onSessionChange != nil {
			w.onSessionChange(path)
		}
		return
	}

	// Memory/CLAUDE.md files
	if strings.HasSuffix(path, ".md") {
		if w.onMemoryChange != nil {
			w.onMemoryChange(path)
		}
		return
	}
}

func (w *Watcher) Close() error {
	return w.fsw.Close()
}
