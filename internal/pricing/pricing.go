package pricing

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

//go:embed embedded.json
var embeddedFS embed.FS

const (
	remoteURL    = "https://raw.githubusercontent.com/fabioconcina/claumon/main/pricing.json"
	fetchTimeout = 10 * time.Second
	maxAge       = 24 * time.Hour
)

// ModelPricing holds per-million-token prices in USD.
type ModelPricing struct {
	Input        float64 `json:"input"`
	Output       float64 `json:"output"`
	CacheRead    float64 `json:"cache_read"`
	CacheWrite5m float64 `json:"cache_write_5m"`
	CacheWrite1h float64 `json:"cache_write_1h"`
}

type pricingFile struct {
	Updated string                  `json:"updated"`
	Models  map[string]ModelPricing `json:"models"`
}

// Table is a concurrency-safe pricing lookup.
type Table struct {
	mu     sync.RWMutex
	models map[string]ModelPricing
}

// Get returns pricing for a model ID. The second return value is false if the model
// was not found.
func (t *Table) Get(model string) (ModelPricing, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.models[model]
	return p, ok
}

// Models returns a copy of all model pricing entries.
func (t *Table) Models() map[string]ModelPricing {
	t.mu.RLock()
	defer t.mu.RUnlock()
	cp := make(map[string]ModelPricing, len(t.models))
	for k, v := range t.models {
		cp[k] = v
	}
	return cp
}

func (t *Table) update(models map[string]ModelPricing) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.models = models
}

// cachePath returns ~/.claumon/pricing.json
func cachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claumon", "pricing.json")
}

// Load returns a pricing table using the layered strategy:
// 1. Local cache (if fresh, < 24h old)
// 2. Remote fetch from GitHub (cached on success)
// 3. Embedded fallback
//
// configOverrides are optional per-model overrides from config.json.
func Load(configOverrides map[string]ModelPricing) *Table {
	table := &Table{}

	// Start with embedded defaults
	models := loadEmbedded()

	// Try local cache
	if cached, err := loadCacheFile(); err == nil {
		mergeInto(models, cached)
	}

	// Try remote fetch if cache is stale
	if isCacheStale() {
		if remote, err := fetchRemote(); err == nil {
			mergeInto(models, remote)
			saveCache(remote)
		} else {
			log.Printf("[pricing] Remote fetch failed: %v (using cache/embedded)", err)
		}
	}

	// Apply config overrides last (highest priority)
	if configOverrides != nil {
		mergeInto(models, configOverrides)
	}

	table.update(models)
	return table
}

// RefreshAsync fetches fresh pricing from GitHub in the background and updates the table.
func RefreshAsync(table *Table, configOverrides map[string]ModelPricing) {
	go func() {
		remote, err := fetchRemote()
		if err != nil {
			log.Printf("[pricing] Background refresh failed: %v", err)
			return
		}

		saveCache(remote)

		models := loadEmbedded()
		if cached, err := loadCacheFile(); err == nil {
			mergeInto(models, cached)
		}
		if configOverrides != nil {
			mergeInto(models, configOverrides)
		}
		table.update(models)
		log.Printf("[pricing] Background refresh complete (%d models)", len(models))
	}()
}

func loadEmbedded() map[string]ModelPricing {
	data, err := embeddedFS.ReadFile("embedded.json")
	if err != nil {
		log.Printf("[pricing] WARNING: embedded pricing not found: %v", err)
		return make(map[string]ModelPricing)
	}
	var pf pricingFile
	if err := json.Unmarshal(data, &pf); err != nil {
		log.Printf("[pricing] WARNING: embedded pricing parse error: %v", err)
		return make(map[string]ModelPricing)
	}
	return pf.Models
}

func loadCacheFile() (map[string]ModelPricing, error) {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return nil, fmt.Errorf("reading pricing cache: %w", err)
	}
	var pf pricingFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("parse cache: %w", err)
	}
	return pf.Models, nil
}

func isCacheStale() bool {
	info, err := os.Stat(cachePath())
	if err != nil {
		return true // no cache = stale
	}
	return time.Since(info.ModTime()) > maxAge
}

func fetchRemote() (map[string]ModelPricing, error) {
	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Get(remoteURL)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var pf pricingFile
	if err := json.Unmarshal(body, &pf); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	if len(pf.Models) == 0 {
		return nil, fmt.Errorf("empty pricing data")
	}

	return pf.Models, nil
}

func saveCache(models map[string]ModelPricing) {
	pf := pricingFile{
		Updated: time.Now().Format("2006-01-02"),
		Models:  models,
	}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		log.Printf("[pricing] Failed to marshal cache data: %v", err)
		return
	}

	path := cachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Printf("[pricing] Failed to create cache directory: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("[pricing] Failed to write cache file: %v", err)
	}
}

func mergeInto(dst, src map[string]ModelPricing) {
	for k, v := range src {
		dst[k] = v
	}
}
