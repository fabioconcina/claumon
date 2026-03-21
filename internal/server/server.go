package server

import (
	"io/fs"
	"net/http"

	"github.com/fabioconcina/claumon/internal/store"
)

type Server struct {
	Mux      *http.ServeMux
	Broker   *SSEBroker
	Handlers *Handlers
}

func New(claudeDir string, st *store.Store, webFS fs.FS) *Server {
	broker := NewBroker()
	handlers := NewHandlers(claudeDir, st)

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("GET /api/info", handlers.HandleInfo)
	mux.HandleFunc("GET /api/usage", handlers.HandleUsage)
	mux.HandleFunc("GET /api/today", handlers.HandleToday)
	mux.HandleFunc("GET /api/history", handlers.HandleHistory)
	mux.HandleFunc("GET /api/sessions", handlers.HandleSessions)
	mux.HandleFunc("GET /api/memories", handlers.HandleMemories)
	mux.HandleFunc("GET /api/memories/staleness", handlers.HandleMemoriesStaleness)
	mux.HandleFunc("GET /api/memories/graph", handlers.HandleMemoriesGraph)
	mux.HandleFunc("GET /api/memories/consolidation", handlers.HandleMemoriesConsolidation)
	mux.HandleFunc("GET /api/memories/search", handlers.HandleMemoriesSearch)
	mux.HandleFunc("GET /api/sessions/{id}", handlers.HandleSessionDetail)

	// SSE
	mux.Handle("GET /api/events", broker)

	// Static files (embedded)
	mux.Handle("/", http.FileServer(http.FS(webFS)))

	return &Server{
		Mux:      mux,
		Broker:   broker,
		Handlers: handlers,
	}
}
