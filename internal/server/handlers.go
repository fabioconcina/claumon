package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"

	"github.com/fabioconcina/claumon/internal/memory"
	"github.com/fabioconcina/claumon/internal/parser"
	"github.com/fabioconcina/claumon/internal/store"
)

type Handlers struct {
	claudeDir        string
	store            *store.Store
	memories         *MemoryCache
	usageMu          sync.RWMutex
	latestUsage      map[string]interface{}
	Version          string
	SubscriptionType string
	RateLimitTier    string
}

type MemoryCache struct {
	files         []*memory.MemoryFile
	staleness     *memory.StalenessReport
	graph         *memory.GraphData
	consolidation *memory.ConsolidationReport
}

func NewHandlers(claudeDir string, st *store.Store) *Handlers {
	h := &Handlers{
		claudeDir: claudeDir,
		store:     st,
		memories:  &MemoryCache{},
	}
	h.RefreshMemories()
	return h
}

func (h *Handlers) RefreshMemories() {
	files, err := memory.DiscoverAll(h.claudeDir)
	if err == nil {
		h.memories.files = files
		h.memories.staleness = memory.CheckStaleness(files)
		h.memories.graph = memory.BuildGraph(files)
		h.memories.consolidation = memory.FindConsolidation(files)
	}
}

func (h *Handlers) SetLatestUsage(data map[string]interface{}) {
	h.usageMu.Lock()
	h.latestUsage = data
	h.usageMu.Unlock()
}

func (h *Handlers) HandleUsage(w http.ResponseWriter, r *http.Request) {
	h.usageMu.RLock()
	data := h.latestUsage
	h.usageMu.RUnlock()

	if data == nil {
		writeJSON(w, map[string]interface{}{
			"session_pct": 0,
			"weekly_pct":  0,
			"available":   false,
		})
		return
	}
	writeJSON(w, data)
}

func (h *Handlers) HandleToday(w http.ResponseWriter, r *http.Request) {
	// Compute from session files for freshest data
	sessions, err := parser.DiscoverTodaySessions(h.claudeDir)
	if err != nil {
		writeJSON(w, store.DailyAggregate{})
		return
	}

	a := parser.AggregateSessions(sessions)
	writeJSON(w, store.DailyAggregate{
		InputTokens:       a.InputTokens,
		OutputTokens:      a.OutputTokens,
		CacheReadTokens:   a.CacheReadTokens,
		CacheCreateTokens: a.CacheCreateTokens,
		CostUSD:           a.CostUSD,
		SessionCount:      a.SessionCount,
		MessageCount:      a.MessageCount,
	})
}

func (h *Handlers) HandleHistory(w http.ResponseWriter, r *http.Request) {
	days := 14
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}

	history, err := h.store.GetHistory(days)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if history == nil {
		history = []store.DailyAggregate{}
	}
	writeJSON(w, history)
}

func (h *Handlers) HandleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := parser.DiscoverTodaySessions(h.claudeDir)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []*parser.SessionSummary{}
	}
	writeJSON(w, sessions)
}

func (h *Handlers) HandleMemories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.memories.files)
}

func (h *Handlers) HandleMemoriesStaleness(w http.ResponseWriter, r *http.Request) {
	if h.memories.staleness == nil {
		writeJSON(w, &memory.StalenessReport{Alerts: []memory.StalenessAlert{}, CheckedAt: 0})
		return
	}
	writeJSON(w, h.memories.staleness)
}

func (h *Handlers) HandleMemoriesGraph(w http.ResponseWriter, r *http.Request) {
	if h.memories.graph == nil {
		writeJSON(w, &memory.GraphData{
			Nodes: []memory.GraphNode{}, Edges: []memory.GraphEdge{}, Groups: []memory.GraphGroup{},
		})
		return
	}
	writeJSON(w, h.memories.graph)
}

func (h *Handlers) HandleMemoriesConsolidation(w http.ResponseWriter, r *http.Request) {
	if h.memories.consolidation == nil {
		writeJSON(w, &memory.ConsolidationReport{Groups: []memory.ConsolidationGroup{}, CheckedAt: 0})
		return
	}
	writeJSON(w, h.memories.consolidation)
}

func (h *Handlers) HandleMemoriesSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	results := memory.SearchMemories(h.memories.files, q)
	if results == nil {
		results = []*memory.MemoryFile{}
	}
	writeJSON(w, results)
}

func (h *Handlers) HandleSessionDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, "missing session id", http.StatusBadRequest)
		return
	}

	path := parser.FindSessionFile(h.claudeDir, id)
	if path == "" {
		writeJSONError(w, "session not found", http.StatusNotFound)
		return
	}

	messages, err := parser.ParseSessionDetail(path)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if messages == nil {
		messages = []parser.SessionMessage{}
	}
	writeJSON(w, messages)
}

func (h *Handlers) HandleInfo(w http.ResponseWriter, r *http.Request) {
	isAPI := h.SubscriptionType == "" || h.SubscriptionType == "api"
	writeJSON(w, map[string]interface{}{
		"version":           h.Version,
		"subscription_type": h.SubscriptionType,
		"rate_limit_tier":   h.RateLimitTier,
		"is_api_billing":    isAPI,
	})
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
