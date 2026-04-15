package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/fabioconcina/claumon/internal/auth"
	"github.com/fabioconcina/claumon/internal/memory"
	"github.com/fabioconcina/claumon/internal/parser"
	"github.com/fabioconcina/claumon/internal/store"
)

type Handlers struct {
	claudeDir        string
	store            *store.Store
	memMu            sync.RWMutex
	memories         memoryCache
	usageMu          sync.RWMutex
	latestUsage      map[string]interface{}
	lastPollOK       time.Time
	lastPollError    string
	AuthProvider     *auth.Provider
	Version          string
	SubscriptionType string
	RateLimitTier    string
	StuckThreshold   time.Duration
}

type memoryCache struct {
	files         []*memory.MemoryFile
	staleness     *memory.StalenessReport
	graph         *memory.GraphData
	consolidation *memory.ConsolidationReport
	health        *memory.HealthReport
}

func NewHandlers(claudeDir string, st *store.Store) *Handlers {
	h := &Handlers{
		claudeDir: claudeDir,
		store:     st,
	}
	h.RefreshMemories()
	return h
}

func (h *Handlers) RefreshMemories() {
	files, err := memory.DiscoverAll(h.claudeDir)
	if err != nil {
		log.Printf("[memory] Failed to discover memories: %v", err)
		return
	}
	mc := memoryCache{
		files:         files,
		staleness:     memory.CheckStaleness(files),
		graph:         memory.BuildGraph(files),
		consolidation: memory.FindConsolidation(files),
		health:        memory.ScoreHealth(files),
	}
	h.memMu.Lock()
	h.memories = mc
	h.memMu.Unlock()
}

// getMemories returns a snapshot of the memory cache under the read lock.
func (h *Handlers) getMemories() memoryCache {
	h.memMu.RLock()
	defer h.memMu.RUnlock()
	return h.memories
}

func (h *Handlers) SetLatestUsage(data map[string]interface{}) {
	h.usageMu.Lock()
	h.latestUsage = data
	h.lastPollOK = time.Now()
	h.lastPollError = ""
	h.usageMu.Unlock()
}

func (h *Handlers) SetPollError(errMsg string) {
	h.usageMu.Lock()
	h.lastPollError = errMsg
	h.usageMu.Unlock()
}

func (h *Handlers) HandleUsage(w http.ResponseWriter, r *http.Request) {
	h.usageMu.RLock()
	data := h.latestUsage
	lastOK := h.lastPollOK
	pollErr := h.lastPollError
	h.usageMu.RUnlock()

	if data == nil {
		writeJSON(w, map[string]interface{}{
			"session_pct": 0,
			"weekly_pct":  0,
			"available":   false,
			"poll_error":  pollErr,
		})
		return
	}
	// Clone to avoid mutating cached data
	resp := make(map[string]interface{}, len(data)+2)
	for k, v := range data {
		resp[k] = v
	}
	if !lastOK.IsZero() {
		resp["last_poll_at"] = lastOK.Unix()
	}
	if pollErr != "" {
		resp["poll_error"] = pollErr
	}
	writeJSON(w, resp)
}

func (h *Handlers) HandleToday(w http.ResponseWriter, r *http.Request) {
	// Compute from session files for freshest data
	sessions, err := parser.DiscoverTodaySessions(h.claudeDir)
	if err != nil {
		writeJSON(w, parser.SessionAggregate{})
		return
	}
	writeJSON(w, parser.AggregateSessions(sessions))
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
	var sessions []*parser.SessionSummary
	var err error

	if r.URL.Query().Get("range") == "all" {
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		sessions, err = parser.DiscoverRecentSessions(h.claudeDir, limit)
	} else {
		sessions, err = parser.DiscoverTodaySessions(h.claudeDir)
	}
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []*parser.SessionSummary{}
	}
	parser.EnrichSessionsWithProcessStatus(sessions, h.claudeDir, h.StuckThreshold)
	writeJSON(w, sessions)
}

func (h *Handlers) HandleKillSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, "missing session id", http.StatusBadRequest)
		return
	}

	if err := parser.KillSession(h.claudeDir, id); err != nil {
		writeJSONError(w, err.Error(), http.StatusNotFound)
		return
	}
	log.Printf("[process] Kill requested for session %s", id)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) HandleProcesses(w http.ResponseWriter, r *http.Request) {
	procs := parser.DiscoverPIDFiles(h.claudeDir)
	if procs == nil {
		procs = []parser.PIDInfo{}
	}
	writeJSON(w, procs)
}

func (h *Handlers) HandleKillProcess(w http.ResponseWriter, r *http.Request) {
	pidStr := r.PathValue("pid")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		writeJSONError(w, "invalid pid", http.StatusBadRequest)
		return
	}

	if err := parser.KillProcess(h.claudeDir, pid); err != nil {
		writeJSONError(w, err.Error(), http.StatusNotFound)
		return
	}
	log.Printf("[process] Kill requested for PID %d", pid)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handlers) HandleMemories(w http.ResponseWriter, r *http.Request) {
	mc := h.getMemories()
	files := mc.files
	if files == nil {
		files = []*memory.MemoryFile{}
	}
	writeJSON(w, files)
}

func (h *Handlers) HandleMemoriesStaleness(w http.ResponseWriter, r *http.Request) {
	mc := h.getMemories()
	if mc.staleness == nil {
		writeJSON(w, &memory.StalenessReport{Alerts: []memory.StalenessAlert{}, CheckedAt: 0})
		return
	}
	writeJSON(w, mc.staleness)
}

func (h *Handlers) HandleMemoriesGraph(w http.ResponseWriter, r *http.Request) {
	mc := h.getMemories()
	if mc.graph == nil {
		writeJSON(w, &memory.GraphData{
			Nodes: []memory.GraphNode{}, Edges: []memory.GraphEdge{}, Groups: []memory.GraphGroup{},
		})
		return
	}
	writeJSON(w, mc.graph)
}

func (h *Handlers) HandleMemoriesConsolidation(w http.ResponseWriter, r *http.Request) {
	mc := h.getMemories()
	if mc.consolidation == nil {
		writeJSON(w, &memory.ConsolidationReport{Groups: []memory.ConsolidationGroup{}, CheckedAt: 0})
		return
	}
	writeJSON(w, mc.consolidation)
}

func (h *Handlers) HandleMemoriesHealth(w http.ResponseWriter, r *http.Request) {
	mc := h.getMemories()
	if mc.health == nil {
		writeJSON(w, &memory.HealthReport{
			Scores:     []memory.HealthScore{},
			GradeCount: map[string]int{},
			CheckedAt:  0,
		})
		return
	}
	writeJSON(w, mc.health)
}

func (h *Handlers) HandleMemoriesSearch(w http.ResponseWriter, r *http.Request) {
	mc := h.getMemories()
	q := r.URL.Query().Get("q")
	results := memory.SearchMemories(mc.files, q)
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
	info := map[string]interface{}{
		"version":           h.Version,
		"subscription_type": h.SubscriptionType,
		"rate_limit_tier":   h.RateLimitTier,
		"is_api_billing":    isAPI,
	}
	if h.AuthProvider != nil {
		status, msg := h.AuthProvider.Status()
		info["auth_status"] = status
		info["auth_message"] = msg
	}
	writeJSON(w, info)
}

func (h *Handlers) HandleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if h.AuthProvider == nil {
		writeJSON(w, map[string]string{"status": auth.AuthOK, "message": ""})
		return
	}
	status, msg := h.AuthProvider.Status()
	writeJSON(w, map[string]string{"status": status, "message": msg})
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
