package server

import (
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/fabioconcina/claumon/internal/store"
)

type Server struct {
	Mux      http.Handler
	Broker   *SSEBroker
	Handlers *Handlers
}

// New builds the dashboard server. allowedHosts lists extra hostnames the
// server answers to besides loopback ones, for reverse proxies that forward
// the browser's Host header unchanged.
func New(claudeDir string, st *store.Store, webFS fs.FS, allowedHosts []string) *Server {
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
	mux.HandleFunc("GET /api/memories/trash", handlers.HandleMemoriesTrash)
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
		Mux:      secureLocalRequests(mux, allowedHosts),
		Broker:   broker,
		Handlers: handlers,
	}
}

// secureLocalRequests adds defense in depth around a server that is intended
// to be used from its own origin. Direct non-browser clients remain supported,
// but a web page from another origin can neither read local data nor trigger
// process or file mutations.
func secureLocalRequests(next http.Handler, allowedHosts []string) http.Handler {
	extra := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		if name := hostName(h); name != "" {
			extra[name] = true
		}
	}
	var logRejectOnce sync.Once

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}

		// DNS rebinding: a hostile page can point its own domain at
		// 127.0.0.1, which makes it same-origin with this server as far as the
		// browser is concerned, so the Origin check below would pass. The Host
		// header still carries the attacker's domain and a page cannot
		// override it, so only names that really mean this machine get in.
		if !hostAllowed(r.Host, extra) {
			logRejectOnce.Do(func() {
				log.Printf("[server] Rejected request for host %q (logged once). If this is your reverse proxy's hostname, add it to allowed_hosts in config.json", r.Host)
			})
			http.Error(w, "host not allowed: if this is your reverse proxy's hostname, add it to allowed_hosts in claumon's config.json", http.StatusForbidden)
			return
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

// hostAllowed reports whether a Host header names this machine: localhost,
// a loopback IP literal, or one of the configured extra hostnames. IP
// literals are safe to accept wholesale because DNS rebinding needs a domain
// name the attacker controls.
func hostAllowed(hostport string, extra map[string]bool) bool {
	host := hostName(hostport)
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return extra[host]
}

// hostName returns the lowercase hostname of a Host header value, without
// port, IPv6 brackets or trailing dot.
func hostName(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	return strings.ToLower(host)
}

// forwardedValue returns the first element of a possibly comma-separated
// X-Forwarded-* header (each intermediate proxy appends its own value).
func forwardedValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
