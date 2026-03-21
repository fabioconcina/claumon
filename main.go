package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fabioconcina/claumon/internal/api"
	"github.com/fabioconcina/claumon/internal/auth"
	"github.com/fabioconcina/claumon/internal/parser"
	"github.com/fabioconcina/claumon/internal/server"
	"github.com/fabioconcina/claumon/internal/store"
	"github.com/fabioconcina/claumon/internal/watcher"
)

//go:embed web
var webFS embed.FS

type Config struct {
	Port             int    `json:"port"`
	PollIntervalSecs int    `json:"poll_interval_seconds"`
	CredentialsPath  string `json:"credentials_path"`
	ClaudeDir        string `json:"claude_dir"`
	DBPath           string `json:"db_path"`
}

func defaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Port:             3131,
		PollIntervalSecs: 120,
		CredentialsPath:  filepath.Join(home, ".claude", ".credentials.json"),
		ClaudeDir:        filepath.Join(home, ".claude"),
		DBPath:           filepath.Join(home, ".claumon", "usage.db"),
	}
}

func loadConfig() Config {
	cfg := defaultConfig()
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".claumon", "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)

	// Re-apply defaults for zero values
	if cfg.Port == 0 {
		cfg.Port = 3131
	}
	if cfg.PollIntervalSecs == 0 {
		cfg.PollIntervalSecs = 120
	}
	if cfg.ClaudeDir == "" {
		cfg.ClaudeDir = filepath.Join(home, ".claude")
	}
	if cfg.DBPath == "" {
		cfg.DBPath = filepath.Join(home, ".claumon", "usage.db")
	}
	return cfg
}

var version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	cfg := loadConfig()

	log.Printf("claumon starting — port=%d claude_dir=%s", cfg.Port, cfg.ClaudeDir)

	// Load credentials
	creds, err := auth.Load(cfg.ClaudeDir, cfg.CredentialsPath)
	if err != nil {
		log.Printf("WARNING: Could not load credentials: %v", err)
		log.Printf("Usage API will be unavailable. Run 'claude /login' to authenticate.")
		creds = &auth.Credentials{}
	} else {
		log.Printf("Loaded credentials: subscription=%s tier=%s", creds.SubscriptionType, creds.RateLimitTier)
	}

	// Open SQLite store
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer st.Close()
	log.Printf("Database opened at %s", cfg.DBPath)

	// Setup HTTP server
	webContent, _ := fs.Sub(webFS, "web")
	srv := server.New(cfg.ClaudeDir, st, webContent)
	srv.Handlers.Version = version
	srv.Handlers.SubscriptionType = creds.SubscriptionType
	srv.Handlers.RateLimitTier = creds.RateLimitTier

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start SSE broker
	go srv.Broker.Run(ctx)

	// Start usage poller
	if creds.AccessToken != "" {
		apiClient := api.NewClient(creds)
		go pollUsage(ctx, apiClient, st, srv.Broker, srv.Handlers, time.Duration(cfg.PollIntervalSecs)*time.Second)
	}

	// Start file watcher
	w, err := watcher.New(cfg.ClaudeDir)
	if err != nil {
		log.Printf("WARNING: File watcher failed to start: %v", err)
	} else {
		w.OnSessionChange(func(path string) {
			log.Printf("[watcher] Session changed: %s", filepath.Base(path))
			sessions, err := parser.DiscoverTodaySessions(cfg.ClaudeDir)
			if err == nil {
				data, _ := json.Marshal(sessions)
				srv.Broker.Send(server.SSEEvent{Event: "sessions", Data: string(data)})
			}
			// Update daily aggregate
			updateDailyAggregate(cfg.ClaudeDir, st)
		})

		w.OnMemoryChange(func(path string) {
			log.Printf("[watcher] Memory changed: %s", filepath.Base(path))
			srv.Handlers.RefreshMemories()
			evt := map[string]string{"path": path}
			data, _ := json.Marshal(evt)
			srv.Broker.Send(server.SSEEvent{Event: "memory_changed", Data: string(data)})
		})

		go w.Start(ctx)
	}

	// Historical backfill (runs once at startup, in background)
	go backfillHistory(cfg.ClaudeDir, st)

	// Initial daily aggregate
	updateDailyAggregate(cfg.ClaudeDir, st)

	// HTTP server
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: srv.Mux,
	}

	go func() {
		log.Printf("Dashboard available at http://localhost:%d", cfg.Port)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer shutdownCancel()
	httpServer.Shutdown(shutdownCtx)

	if w != nil {
		w.Close()
	}
	log.Println("Bye!")
}

func pollUsage(ctx context.Context, client *api.Client, st *store.Store, broker *server.SSEBroker, handlers *server.Handlers, interval time.Duration) {
	// Initial fetch
	fetchAndBroadcastUsage(ctx, client, st, broker, handlers)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fetchAndBroadcastUsage(ctx, client, st, broker, handlers)
		}
	}
}

func fetchAndBroadcastUsage(ctx context.Context, client *api.Client, st *store.Store, broker *server.SSEBroker, handlers *server.Handlers) {
	usage, err := client.FetchUsage(ctx)
	if err != nil {
		log.Printf("[poll] Usage fetch error: %v", err)
		return
	}

	// Save snapshot
	st.SaveUsageSnapshot(usage.SessionPercent, usage.WeeklyPercent, usage.SessionResetAt, usage.WeeklyResetAt, usage.Raw)

	// Broadcast to SSE clients
	evt := map[string]interface{}{
		"session_pct":   usage.SessionPercent,
		"weekly_pct":    usage.WeeklyPercent,
		"session_reset": formatDuration(usage.SessionResetDuration()),
		"weekly_reset":  formatDuration(usage.WeeklyResetDuration()),
	}
	if usage.WeeklySonnetPct != nil {
		evt["weekly_sonnet_pct"] = *usage.WeeklySonnetPct
		evt["weekly_sonnet_reset"] = formatDuration(parseDuration(usage.WeeklySonnetReset))
	}
	if usage.WeeklyOpusPct != nil {
		evt["weekly_opus_pct"] = *usage.WeeklyOpusPct
		evt["weekly_opus_reset"] = formatDuration(parseDuration(usage.WeeklyOpusReset))
	}
	if usage.ExtraUsageEnabled {
		evt["extra_usage_enabled"] = true
		if usage.ExtraUsageLimit != nil {
			evt["extra_usage_limit"] = *usage.ExtraUsageLimit
		}
		if usage.ExtraUsageUsed != nil {
			evt["extra_usage_used"] = *usage.ExtraUsageUsed
		}
	}
	data, _ := json.Marshal(evt)
	broker.Send(server.SSEEvent{Event: "usage", Data: string(data)})
	handlers.SetLatestUsage(evt)
	log.Printf("[poll] Usage: session=%.1f%% weekly=%.1f%%", usage.SessionPercent, usage.WeeklyPercent)
}

func parseDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	d := time.Until(t)
	if d < 0 {
		return 0
	}
	return d
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func updateDailyAggregate(claudeDir string, st *store.Store) {
	sessions, err := parser.DiscoverTodaySessions(claudeDir)
	if err != nil {
		return
	}

	today := time.Now().Format("2006-01-02")
	var agg store.DailyAggregate
	agg.Date = today

	seen := make(map[string]bool)
	for _, s := range sessions {
		agg.InputTokens += s.InputTokens
		agg.OutputTokens += s.OutputTokens
		agg.CacheReadTokens += s.CacheReadTokens
		agg.CacheCreateTokens += s.CacheCreateTokens
		agg.CostUSD += s.EstimatedCostUSD
		agg.MessageCount += s.MessageCount
		if !seen[s.ID] {
			seen[s.ID] = true
			agg.SessionCount++
		}
	}

	st.UpsertDailyAggregate(agg)
}

func backfillHistory(claudeDir string, st *store.Store) {
	log.Println("[backfill] Scanning all sessions for historical data...")

	sessions, err := parser.DiscoverSessions(claudeDir)
	if err != nil {
		log.Printf("[backfill] Error discovering sessions: %v", err)
		return
	}

	// Group sessions by date
	byDate := make(map[string][]*parser.SessionSummary)
	for _, s := range sessions {
		date := s.LastActivity.Format("2006-01-02")
		if date == "0001-01-01" {
			continue
		}
		byDate[date] = append(byDate[date], s)
	}

	count := 0
	for date, dateSessions := range byDate {
		var agg store.DailyAggregate
		agg.Date = date

		seen := make(map[string]bool)
		for _, s := range dateSessions {
			agg.InputTokens += s.InputTokens
			agg.OutputTokens += s.OutputTokens
			agg.CacheReadTokens += s.CacheReadTokens
			agg.CacheCreateTokens += s.CacheCreateTokens
			agg.CostUSD += s.EstimatedCostUSD
			agg.MessageCount += s.MessageCount
			if !seen[s.ID] {
				seen[s.ID] = true
				agg.SessionCount++
			}
		}

		st.UpsertDailyAggregate(agg)
		count++
	}

	log.Printf("[backfill] Done: %d days from %d sessions", count, len(sessions))
}
