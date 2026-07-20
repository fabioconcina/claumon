package server

import (
	"io/fs"
	"net/http"
	"net/url"
	"strings"

	"github.com/fabioconcina/claumon/internal/store"
)

type Server struct {
	Mux      http.Handler
	Broker   *SSEBroker
	Handlers *Handlers
}

func New(claudeDir string, st *store.Store, webFS fs.FS) *Server {
	broker := NewBroker()
	handlers := NewHandlers(claudeDir, st)

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("GET /api/info", handlers.HandleInfo)
	mux.HandleFunc("GET /api/auth/status", handlers.HandleAuthStatus)
	mux.HandleFunc("GET /api/usage", handlers.HandleUsage)
	mux.HandleFunc("GET /api/forecast", handlers.HandleForecast)
	mux.HandleFunc("GET /api/forecast/sample", handlers.HandleForecastSample)
	mux.HandleFunc("GET /api/today", handlers.HandleToday)
	mux.HandleFunc("GET /api/heatmap", handlers.HandleHeatmap)
	mux.HandleFunc("GET /api/history", handlers.HandleHistory)
	mux.HandleFunc("GET /api/sessions", handlers.HandleSessions)
	mux.HandleFunc("GET /api/memories", handlers.HandleMemories)
	mux.HandleFunc("GET /api/memories/staleness", handlers.HandleMemoriesStaleness)
	mux.HandleFunc("GET /api/memories/graph", handlers.HandleMemoriesGraph)
	mux.HandleFunc("GET /api/memories/consolidation", handlers.HandleMemoriesConsolidation)
	mux.HandleFunc("GET /api/memories/health", handlers.HandleMemoriesHealth)
	mux.HandleFunc("GET /api/memories/search", handlers.HandleMemoriesSearch)
	mux.HandleFunc("POST /api/memories/delete", handlers.HandleDeleteMemory)
	mux.HandleFunc("POST /api/memories/restore", handlers.HandleRestoreMemory)
	mux.HandleFunc("GET /api/sessions/{id}", handlers.HandleSessionDetail)
	mux.HandleFunc("POST /api/sessions/{id}/kill", handlers.HandleKillSession)
	mux.HandleFunc("GET /api/processes", handlers.HandleProcesses)
	mux.HandleFunc("POST /api/processes/{pid}/kill", handlers.HandleKillProcess)

	// SSE
	mux.Handle("GET /api/events", broker)

	// Static files (embedded)
	mux.Handle("/", http.FileServer(http.FS(webFS)))

	return &Server{
		Mux:      secureLocalRequests(mux),
		Broker:   broker,
		Handlers: handlers,
	}
}

// secureLocalRequests adds defense in depth around a server that is intended
// to be used from its own origin. Direct non-browser clients remain supported,
// but a web page from another origin cannot trigger process or file mutations.
func secureLocalRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !requestIsSameOrigin(r) {
				http.Error(w, "cross-origin request denied", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func requestIsSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Modern browsers identify requests initiated by a different site even
		// in cases where Origin is omitted. CLI/API clients send neither header.
		return !strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site")
	}

	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	// Behind a TLS-terminating reverse proxy (the documented remote-access
	// path) the browser's Origin carries the proxy's external scheme and host,
	// not the loopback listener's. Honor the standard forwarded headers: a
	// cross-origin browser request cannot set them (custom headers require a
	// CORS preflight, which this server never grants), so trusting them does
	// not weaken the guard.
	expectedScheme := "http"
	if r.TLS != nil {
		expectedScheme = "https"
	}
	if proto := forwardedValue(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		expectedScheme = proto
	}
	expectedHost := r.Host
	if host := forwardedValue(r.Header.Get("X-Forwarded-Host")); host != "" {
		expectedHost = host
	}
	return strings.EqualFold(u.Scheme, expectedScheme) && strings.EqualFold(u.Host, expectedHost)
}

// forwardedValue returns the first element of a possibly comma-separated
// X-Forwarded-* header (each intermediate proxy appends its own value).
func forwardedValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
