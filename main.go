package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/fabioconcina/claumon/internal/api"
	"github.com/fabioconcina/claumon/internal/auth"
	"github.com/fabioconcina/claumon/internal/parser"
	"github.com/fabioconcina/claumon/internal/pricing"
	"github.com/fabioconcina/claumon/internal/server"
	"github.com/fabioconcina/claumon/internal/service"
	"github.com/fabioconcina/claumon/internal/store"
	"github.com/fabioconcina/claumon/internal/updater"
	"github.com/fabioconcina/claumon/internal/watcher"
)

//go:embed web
var webFS embed.FS

type Config struct {
	Port             int                                `json:"port"`
	PollIntervalSecs int                                `json:"poll_interval_seconds"`
	CredentialsPath  string                             `json:"credentials_path"`
	ClaudeDir        string                             `json:"claude_dir"`
	DBPath           string                             `json:"db_path"`
	RetentionDays    int                                `json:"retention_days"`
	PricingOverrides    map[string]pricing.ModelPricing `json:"pricing_overrides,omitempty"`
	StuckThresholdMins int                            `json:"stuck_threshold_minutes"`
}

func defaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Port:             3131,
		PollIntervalSecs: 120,
		CredentialsPath:  filepath.Join(home, ".claude", ".credentials.json"),
		ClaudeDir:        filepath.Join(home, ".claude"),
		DBPath:           filepath.Join(home, ".claumon", "usage.db"),
		RetentionDays:    90,
	}
}

func loadConfig() Config {
	cfg := defaultConfig()
	configPath := filepath.Join(filepath.Dir(cfg.DBPath), "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("[config] Failed to parse config file: %v (using defaults)", err)
		return cfg
	}

	// Re-apply defaults for zero values
	defaults := defaultConfig()
	if cfg.Port == 0 {
		cfg.Port = defaults.Port
	}
	if cfg.PollIntervalSecs == 0 {
		cfg.PollIntervalSecs = defaults.PollIntervalSecs
	}
	if cfg.ClaudeDir == "" {
		cfg.ClaudeDir = defaults.ClaudeDir
	}
	if cfg.DBPath == "" {
		cfg.DBPath = defaults.DBPath
	}
	if cfg.RetentionDays == 0 {
		cfg.RetentionDays = defaults.RetentionDays
	}
	if cfg.StuckThresholdMins == 0 {
		cfg.StuckThresholdMins = 10
	}
	return cfg
}

var version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println(version)
			return
		case "update":
			runUpdate()
			return
		case "service":
			runService()
			return
		}
	}

	openBrowser := flag.Bool("open", false, "Open dashboard in browser on startup")
	flag.Parse()
	cfg := loadConfig()

	log.Printf("[startup] claumon starting — port=%d claude_dir=%s", cfg.Port, cfg.ClaudeDir)

	// Load pricing table (embedded → cache → remote → config overrides)
	pricingTable := pricing.Load(cfg.PricingOverrides)
	parser.SetPricingTable(pricingTable)
	log.Printf("[pricing] loaded pricing for %d models", len(pricingTable.Models()))

	// Load credentials with auto-reload support
	provider := auth.NewProvider(cfg.ClaudeDir, cfg.CredentialsPath)
	creds := provider.Credentials()

	// Open SQLite store
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("[startup] failed to open database: %v", err)
	}
	defer st.Close()
	log.Printf("[startup] database opened at %s", cfg.DBPath)

	// Setup HTTP server
	webContent, _ := fs.Sub(webFS, "web")
	srv := server.New(cfg.ClaudeDir, st, webContent)
	srv.Handlers.AuthProvider = provider
	srv.Handlers.Version = version
	srv.Handlers.SubscriptionType = creds.SubscriptionType
	srv.Handlers.RateLimitTier = creds.RateLimitTier
	srv.Handlers.StuckThreshold = time.Duration(cfg.StuckThresholdMins) * time.Minute

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start SSE broker
	go srv.Broker.Run(ctx)

	// Start usage poller (provider handles credential reload on expiry)
	if provider.HasToken() {
		apiClient := api.NewClient(provider)
		go pollUsage(ctx, apiClient, provider, st, srv.Broker, srv.Handlers, time.Duration(cfg.PollIntervalSecs)*time.Second)
	}

	// Start file watcher
	w, err := watcher.New(cfg.ClaudeDir)
	if err != nil {
		log.Printf("[watcher] Failed to start: %v", err)
	} else {
		stuckThreshold := time.Duration(cfg.StuckThresholdMins) * time.Minute
		w.OnSessionChange(func(path string) {
			log.Printf("[watcher] Session changed: %s", filepath.Base(path))
			sessions, err := parser.DiscoverTodaySessions(cfg.ClaudeDir)
			if err == nil {
				parser.EnrichSessionsWithProcessStatus(sessions, cfg.ClaudeDir, stuckThreshold)
				srv.Broker.SendJSON("sessions", sessions)
			}
			// Update daily aggregate
			updateDailyAggregate(cfg.ClaudeDir, st)
		})

		w.OnMemoryChange(func(path string) {
			log.Printf("[watcher] Memory changed: %s", filepath.Base(path))
			srv.Handlers.RefreshMemories()
			srv.Broker.SendJSON("memory_changed", map[string]string{"path": path})
		})

		go w.Start(ctx)
	}

	// Daily maintenance: refresh pricing and prune old data
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pricing.RefreshAsync(pricingTable, cfg.PricingOverrides)
				if err := st.Prune(cfg.RetentionDays); err != nil {
					log.Printf("[prune] Error: %v", err)
				}
			}
		}
	}()

	// Historical backfill and initial prune (runs once at startup, in background)
	go func() {
		backfillHistory(cfg.ClaudeDir, st)
		if err := st.Prune(cfg.RetentionDays); err != nil {
			log.Printf("[prune] Error: %v", err)
		}
	}()

	// Initial daily aggregate
	updateDailyAggregate(cfg.ClaudeDir, st)

	// HTTP server
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: srv.Mux,
	}

	dashboardURL := fmt.Sprintf("http://localhost:%d", cfg.Port)
	go func() {
		log.Printf("[startup] dashboard available at %s", dashboardURL)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("[startup] HTTP server error: %v", err)
		}
	}()

	if *openBrowser {
		// Small delay to let the server start, then open browser
		go func() {
			time.Sleep(300 * time.Millisecond)
			if err := openURL(dashboardURL); err != nil {
				log.Printf("[browser] Failed to open: %v", err)
			}
		}()
	}

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Printf("[shutdown] stopping...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer shutdownCancel()
	httpServer.Shutdown(shutdownCtx)

	if w != nil {
		w.Close()
	}
	log.Printf("[shutdown] bye")
}

func pollUsage(ctx context.Context, client *api.Client, provider *auth.Provider, st *store.Store, broker *server.SSEBroker, handlers *server.Handlers, interval time.Duration) {
	// Initial fetch with a small delay to avoid hitting the API immediately on every restart
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}
	p := &poller{
		client: client, provider: provider, st: st, broker: broker, handlers: handlers,
		interval: interval, backoff: interval, lastAuthOK: true,
	}

	p.fetch(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(p.backoff):
			p.fetch(ctx)
		}
	}
}

type poller struct {
	client      *api.Client
	provider    *auth.Provider
	st          *store.Store
	broker      *server.SSEBroker
	handlers    *server.Handlers
	interval    time.Duration
	backoff     time.Duration
	lastAuthOK  bool
	authWaiting bool
}

const authWaitBackoff = 30 * time.Second

// enterAuthWait flips the poller into the "waiting for credentials" state.
// Idempotent: repeated calls don't relog.
func (p *poller) enterAuthWait() {
	if !p.authWaiting {
		log.Printf("[poll] Auth expired, waiting for credentials to refresh (checking every 30s)")
		p.handlers.SetPollError("auth expired — waiting for credentials to refresh")
		p.authWaiting = true
	}
	p.backoff = authWaitBackoff
}

// authOK reports whether the provider currently holds a usable token.
func (p *poller) authOK() bool {
	status, _ := p.provider.Status()
	return status == auth.AuthOK
}

func (p *poller) fetch(ctx context.Context) {
	// When auth is expired, don't hit the API — just try reloading credentials
	if !p.authOK() {
		if err := p.provider.Reload(); err != nil || !p.authOK() {
			p.enterAuthWait()
			p.lastAuthOK = broadcastAuthStatus(p.provider, p.broker, p.lastAuthOK)
			return
		}
		log.Printf("[poll] Auth recovered, resuming API polling")
		p.authWaiting = false
	}

	err := fetchAndBroadcastUsage(ctx, p.client, p.st, p.broker, p.handlers)
	p.lastAuthOK = broadcastAuthStatus(p.provider, p.broker, p.lastAuthOK)
	if err == nil {
		p.backoff = p.interval
		p.authWaiting = false
		return
	}

	var authErr *api.AuthError
	if errors.As(err, &authErr) {
		p.enterAuthWait()
		return
	}
	p.handlers.SetPollError(err.Error())
	p.backoff = retryBackoff(err, min(p.backoff*2, 10*time.Minute))
	log.Printf("[poll] Backing off to %v", p.backoff)
}

func broadcastAuthStatus(provider *auth.Provider, broker *server.SSEBroker, lastAuthOK bool) bool {
	status, msg := provider.Status()
	isOK := status == auth.AuthOK

	// Always broadcast when not OK so the frontend stays informed
	if isOK != lastAuthOK {
		if isOK {
			log.Printf("[auth] Credentials recovered")
		} else {
			log.Printf("[auth] %s", msg)
		}
	}
	if !isOK || isOK != lastAuthOK {
		broker.SendJSON("auth_status", map[string]string{"status": status, "message": msg})
	}

	return isOK
}

// retryBackoff returns the Retry-After duration from a RateLimitError if it exceeds
// the default backoff, otherwise returns the default.
func retryBackoff(err error, defaultBackoff time.Duration) time.Duration {
	var rle *api.RateLimitError
	if errors.As(err, &rle) && rle.RetryAfter > defaultBackoff {
		return rle.RetryAfter
	}
	return defaultBackoff
}

func fetchAndBroadcastUsage(ctx context.Context, client *api.Client, st *store.Store, broker *server.SSEBroker, handlers *server.Handlers) error {
	usage, err := client.FetchUsage(ctx)
	if err != nil {
		log.Printf("[poll] Usage fetch error: %v", err)
		return err
	}

	// Save snapshot
	if err := st.SaveUsageSnapshot(usage.SessionPercent, usage.WeeklyPercent, usage.SessionResetAt, usage.WeeklyResetAt, usage.Raw); err != nil {
		log.Printf("[poll] Failed to save usage snapshot: %v", err)
	}

	// Broadcast to SSE clients
	evt := buildUsageEvent(usage)
	broker.SendJSON("usage", evt)
	handlers.SetLatestUsage(evt)
	log.Printf("[poll] Usage: session=%.1f%% weekly=%.1f%%", usage.SessionPercent, usage.WeeklyPercent)
	return nil
}

func buildUsageEvent(usage *api.UsageResponse) map[string]interface{} {
	evt := map[string]interface{}{
		"session_pct":   usage.SessionPercent,
		"weekly_pct":    usage.WeeklyPercent,
		"session_reset": formatDuration(usage.SessionResetDuration()),
		"weekly_reset":  formatDuration(usage.WeeklyResetDuration()),
	}
	if usage.WeeklySonnetPct != nil {
		evt["weekly_sonnet_pct"] = *usage.WeeklySonnetPct
		evt["weekly_sonnet_reset"] = formatDuration(api.ParseResetDuration(usage.WeeklySonnetReset))
	}
	if usage.WeeklyOpusPct != nil {
		evt["weekly_opus_pct"] = *usage.WeeklyOpusPct
		evt["weekly_opus_reset"] = formatDuration(api.ParseResetDuration(usage.WeeklyOpusReset))
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
	return evt
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

func toStoreAggregate(date string, sessions []*parser.SessionSummary) store.DailyAggregate {
	a := parser.AggregateSessions(sessions)
	return store.DailyAggregate{
		Date:              date,
		InputTokens:       a.InputTokens,
		OutputTokens:      a.OutputTokens,
		CacheReadTokens:   a.CacheReadTokens,
		CacheCreateTokens: a.CacheCreateTokens,
		CostUSD:           a.CostUSD,
		SessionCount:      a.SessionCount,
		MessageCount:      a.MessageCount,
	}
}

func updateDailyAggregate(claudeDir string, st *store.Store) {
	sessions, err := parser.DiscoverTodaySessions(claudeDir)
	if err != nil {
		log.Printf("[aggregate] Failed to discover today's sessions: %v", err)
		return
	}
	if err := st.UpsertDailyAggregate(toStoreAggregate(time.Now().Format("2006-01-02"), sessions)); err != nil {
		log.Printf("[aggregate] Failed to upsert daily aggregate: %v", err)
	}
}

func backfillHistory(claudeDir string, st *store.Store) {
	log.Printf("[backfill] Scanning all sessions for historical data...")

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
		if err := st.UpsertDailyAggregate(toStoreAggregate(date, dateSessions)); err != nil {
			log.Printf("[backfill] Failed to upsert aggregate for %s: %v", date, err)
		}
		count++
	}

	log.Printf("[backfill] Done: %d days from %d sessions", count, len(sessions))
}

func runUpdate() {
	fmt.Printf("claumon %s — checking for updates...\n", version)

	rel, err := updater.CheckLatest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if !updater.NeedsUpdate(version, rel.TagName) {
		fmt.Printf("Already up to date (%s)\n", version)
		return
	}

	fmt.Printf("New version available: %s → %s\n", version, rel.TagName)
	newVersion, err := updater.Update(rel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Updated to %s\n", newVersion)

	// Restart service if installed
	status, _ := service.Status()
	if status != "not installed" {
		fmt.Print("Restarting service... ")
		if err := service.Restart(); err != nil {
			fmt.Fprintf(os.Stderr, "failed: %v\n", err)
			fmt.Println("Run 'claumon service restart' manually.")
		} else {
			fmt.Println("done")
		}
	}
}

func runService() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: claumon service <install|uninstall|status|restart>")
		os.Exit(1)
	}
	action := os.Args[2]

	switch action {
	case "install":
		execPath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving executable path: %v\n", err)
			os.Exit(1)
		}
		cfg := loadConfig()
		if err := service.Install(execPath); err != nil {
			fmt.Fprintf(os.Stderr, "Install failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("claumon %s — service installed and started (port %d)\n", version, cfg.Port)
		fmt.Printf("Dashboard: http://localhost:%d\n", cfg.Port)
		fmt.Println()
		fmt.Println("claumon will start automatically on login.")
		fmt.Println("To stop:   claumon service uninstall")

	case "uninstall":
		if err := service.Uninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "Uninstall failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service stopped and removed from startup.")

	case "status":
		status, err := service.Status()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Service: %s\n", status)

	case "restart":
		if err := service.Restart(); err != nil {
			fmt.Fprintf(os.Stderr, "Restart failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Service restarted")

	default:
		fmt.Fprintf(os.Stderr, "Unknown action: %s\nUsage: claumon service <install|uninstall|status|restart>\n", action)
		os.Exit(1)
	}
}

func openURL(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()
	case "linux":
		return exec.Command("xdg-open", url).Run()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Run()
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}
