package watcher

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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
	// Debounce map to avoid rapid-fire events
	debounce := make(map[string]time.Time)
	const debounceInterval = 500 * time.Millisecond

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

			// Debounce
			now := time.Now()
			if last, ok := debounce[event.Name]; ok && now.Sub(last) < debounceInterval {
				continue
			}
			debounce[event.Name] = now

			// If a new directory is created under projects, start watching it
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
								w.classifyAndDispatch(filepath.Join(event.Name, de.Name()))
							}
						}
					}
					continue
				}
			}

			w.classifyAndDispatch(event.Name)

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return nil
			}
			log.Printf("[watcher] error: %v", err)
		}
	}
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
