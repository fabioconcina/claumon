package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/fabioconcina/claumon/internal/auth"
	"github.com/fabioconcina/claumon/internal/forecast"
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
	Forecast         *forecast.Service
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
		// Fall through with an empty slice so the cache still has non-nil reports.
		files = nil
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
	// Bucket tokens by message timestamp so resumed sessions don't credit
	// their entire history to today.
	daily, err := parser.DiscoverDailyAggregates(h.claudeDir)
	if err != nil {
		writeJSON(w, parser.SessionAggregate{})
		return
	}
	today := time.Now().Format("2006-01-02")
	writeJSON(w, daily[today])
}

func (h *Handlers) HandleHeatmap(w http.ResponseWriter, r *http.Request) {
	hours, err := parser.HourlyTokensToday(h.claudeDir)
	if err != nil {
		writeJSON(w, [24]int{})
		return
	}
	writeJSON(w, hours)
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
	files := h.getMemories().files
	if files == nil {
		files = []*memory.MemoryFile{}
	}
	writeJSON(w, files)
}

func (h *Handlers) HandleMemoriesStaleness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.getMemories().staleness)
}

func (h *Handlers) HandleMemoriesGraph(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.getMemories().graph)
}

func (h *Handlers) HandleMemoriesConsolidation(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.getMemories().consolidation)
}

func (h *Handlers) HandleMemoriesHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, h.getMemories().health)
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

// HandleForecast returns the latest cached forecast embedded in the usage
// payload. The poll loop owns the actual forecasting; this endpoint is a
// pull-style convenience for clients that don't want to drive SSE.
//
// Response is always {"available": bool, "data": {...}}: data is nil when
// available is false.
func (h *Handlers) HandleForecast(w http.ResponseWriter, r *http.Request) {
	h.usageMu.RLock()
	data := h.latestUsage
	h.usageMu.RUnlock()

	resp := map[string]interface{}{"available": false, "data": nil}
	if data != nil {
		if fc, ok := data["forecast"]; ok {
			resp["available"] = true
			resp["data"] = fc
		}
	}
	writeJSON(w, resp)
}

// HandleForecastSample re-runs the MC for one gauge with trajectories
// collected, and returns the materials needed by the visualization modal.
// On-demand: only fires when the user clicks the projected-percent trigger.
//
// Query: ?gauge=session|weekly (required).
func (h *Handlers) HandleForecastSample(w http.ResponseWriter, r *http.Request) {
	if h.Forecast == nil {
		writeJSON(w, map[string]interface{}{"available": false, "data": nil})
		return
	}
	gaugeStr := r.URL.Query().Get("gauge")
	var gauge forecast.GaugeKind
	switch gaugeStr {
	case "session":
		gauge = forecast.GaugeSession
	case "weekly":
		gauge = forecast.GaugeWeekly
	default:
		writeJSONError(w, "gauge must be session or weekly", http.StatusBadRequest)
		return
	}

	h.usageMu.RLock()
	data := h.latestUsage
	h.usageMu.RUnlock()
	if data == nil {
		writeJSON(w, map[string]interface{}{"available": false, "data": nil})
		return
	}

	var resetAt string
	var uNowPct float64
	if gauge == forecast.GaugeSession {
		resetAt, _ = data["session_reset_at"].(string)
		if v, ok := data["session_pct"].(float64); ok {
			uNowPct = v
		}
	} else {
		resetAt, _ = data["weekly_reset_at"].(string)
		if v, ok := data["weekly_pct"].(float64); ok {
			uNowPct = v
		}
	}
	if resetAt == "" {
		writeJSON(w, map[string]interface{}{"available": false, "data": nil})
		return
	}

	// Forecast against the same threshold the gauge uses, so the popup simulates
	// the same regime and its threshold line / ETA match the gauge's "ETA to X%".
	const thresholdPct = forecast.UIThresholdPct
	// 120 trajectories is enough for legible fog; histogram uses the full K.
	const maxTraj = 120
	// Cap each trajectory's length so weekly (~600 5-min steps) doesn't ship MB.
	const maxSteps = 80

	sample, ok := h.Forecast.SampleFor(gauge, resetAt, uNowPct, time.Now(), thresholdPct, maxTraj, maxSteps)
	if !ok {
		writeJSON(w, map[string]interface{}{"available": false, "data": nil})
		return
	}
	// The popup re-simulates to draw the trajectory fog, but its headline
	// (projected %, 80% CI, ETA) must equal the gauge's. The gauge value is
	// computed once per poll and cached in latestUsage; reuse it so the two can't
	// disagree by re-simulation noise. Falls back to the fresh sample if the
	// cached forecast is absent.
	if fcMap, ok := data["forecast"].(map[string]forecast.Payload); ok {
		if p, ok := fcMap[string(gauge)]; ok {
			sample = applyCachedHeadline(sample, p)
		}
	}
	writeJSON(w, map[string]interface{}{"available": true, "data": sample})
}

// applyCachedHeadline overrides a sample's headline numbers (projected %, 80%
// CI, and the ETA for the matching threshold) with the gauge's last broadcast
// forecast, so the popup reports exactly what the gauge shows instead of a
// freshly simulated interval. Percentages on the cached payload are 0-100; the
// sample carries fractions, hence the /100.
func applyCachedHeadline(s forecast.SamplePayload, p forecast.Payload) forecast.SamplePayload {
	s.F = p.ProjectedPct / 100
	s.CILo = p.Lower80Pct / 100
	s.CIHi = p.Upper80Pct / 100
	for _, e := range p.ETAs {
		if e.ThresholdPct == s.ThresholdPct {
			s.ETA = e
			break
		}
	}
	return s
}

func (h *Handlers) HandleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if h.AuthProvider == nil {
		writeJSON(w, map[string]string{"status": auth.AuthOK, "message": ""})
		return
	}
	status, msg := h.AuthProvider.Status()
	writeJSON(w, map[string]string{"status": status, "message": msg})
}

func setJSONHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	setJSONHeaders(w)
	json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, msg string, status int) {
	setJSONHeaders(w)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
