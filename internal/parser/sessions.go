package parser

import (
	"bufio"
	"encoding/json"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fabioconcina/claumon/internal/pricing"
)

// warnedModels tracks models we've already warned about to avoid log spam.
var warnedModels sync.Map

// pricingTable is the shared pricing table, set via SetPricingTable.
// If nil, a zero-cost fallback is used (should not happen in practice).
var pricingTable *pricing.Table

type SessionSummary struct {
	ID                string    `json:"id"`
	Project           string    `json:"project"`
	Model             string    `json:"model"`
	Title             string    `json:"title"`
	StartTime         time.Time `json:"start_time"`
	LastActivity      time.Time `json:"last_activity"`
	InputTokens       int       `json:"input_tokens"`
	OutputTokens      int       `json:"output_tokens"`
	CacheReadTokens   int       `json:"cache_read_tokens"`
	CacheCreateTokens int       `json:"cache_create_tokens"`
	EstimatedCostUSD  float64   `json:"estimated_cost_usd"`
	MessageCount      int       `json:"message_count"`
	CWD               string    `json:"cwd"`
}

func (s *SessionSummary) TotalTokens() int {
	return s.InputTokens + s.OutputTokens + s.CacheReadTokens + s.CacheCreateTokens
}

// SessionMessage represents a single parsed message from a session for the detail view.
type SessionMessage struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Model     string    `json:"model,omitempty"`
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	TokensIn  int       `json:"tokens_in,omitempty"`
	TokensOut int       `json:"tokens_out,omitempty"`
	CacheRead int       `json:"cache_read,omitempty"`
	ToolUse   string    `json:"tool_use,omitempty"`
}

type jsonlLine struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"sessionId"`
	CWD       string    `json:"cwd"`
	Message   *struct {
		Model   string `json:"model"`
		Role    string `json:"role"`
		Content json.RawMessage `json:"content,omitempty"`
		Usage   *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage,omitempty"`
	} `json:"message,omitempty"`
	Title string `json:"title,omitempty"`
}

func ParseSessionFile(path string) (*SessionSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	summary := &SessionSummary{
		ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"),
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 2*1024*1024)

	var firstUserMsg string

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry jsonlLine
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		ts := entry.Timestamp
		if !ts.IsZero() {
			if summary.StartTime.IsZero() || ts.Before(summary.StartTime) {
				summary.StartTime = ts
			}
			if ts.After(summary.LastActivity) {
				summary.LastActivity = ts
			}
		}

		if entry.SessionID != "" && summary.ID == "" {
			summary.ID = entry.SessionID
		}

		if entry.CWD != "" && summary.CWD == "" {
			summary.CWD = entry.CWD
		}

		switch entry.Type {
		case "ai-title":
			if entry.Title != "" {
				summary.Title = entry.Title
			}
		case "assistant":
			if entry.Message != nil {
				summary.MessageCount++
				if entry.Message.Model != "" {
					summary.Model = entry.Message.Model
				}
				if u := entry.Message.Usage; u != nil {
					summary.InputTokens += u.InputTokens
					summary.OutputTokens += u.OutputTokens
					summary.CacheReadTokens += u.CacheReadInputTokens
					summary.CacheCreateTokens += u.CacheCreationInputTokens
				}
			}
		case "user":
			summary.MessageCount++
			if firstUserMsg == "" && entry.Message != nil {
				firstUserMsg = extractText(entry.Message.Content)
			}
		}
	}

	// Fallback title: first user message, stripped of XML tags, truncated
	if summary.Title == "" && firstUserMsg != "" {
		summary.Title = truncate(stripXMLTags(firstUserMsg), 80)
	}

	summary.EstimatedCostUSD = estimateCost(summary)
	return summary, scanner.Err()
}

// SetPricingTable sets the shared pricing table used for cost estimation.
func SetPricingTable(t *pricing.Table) {
	pricingTable = t
}

func estimateCost(s *SessionSummary) float64 {
	if pricingTable == nil {
		return 0
	}

	model := normalizeModel(s.Model)
	p, ok := pricingTable.Get(model)
	if !ok {
		if _, warned := warnedModels.LoadOrStore(s.Model, true); !warned {
			log.Printf("[pricing] Unknown model %q — using sonnet pricing. Update pricing.json?", s.Model)
		}
		p, _ = pricingTable.Get("claude-sonnet-4-6")
	}

	cost := float64(s.InputTokens)/1e6*p.Input +
		float64(s.OutputTokens)/1e6*p.Output +
		float64(s.CacheReadTokens)/1e6*p.CacheRead +
		float64(s.CacheCreateTokens)/1e6*p.CacheWrite5m

	return math.Round(cost*10000) / 10000
}

func normalizeModel(model string) string {
	if pricingTable == nil {
		return "claude-sonnet-4-6"
	}

	// Strip date suffixes like "claude-sonnet-4-6-20250514"
	for key := range pricingTable.Models() {
		if strings.HasPrefix(model, key) {
			return key
		}
	}
	// Try matching by family — pick the latest known model in each family
	switch {
	case strings.Contains(model, "opus"):
		return "claude-opus-4-6"
	case strings.Contains(model, "haiku"):
		return "claude-haiku-4-5"
	default:
		return "claude-sonnet-4-6"
	}
}

func DiscoverSessions(claudeDir string) ([]*SessionSummary, error) {
	projectsDir := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, err
	}

	var sessions []*SessionSummary
	for _, projEntry := range entries {
		if !projEntry.IsDir() {
			continue
		}
		projPath := filepath.Join(projectsDir, projEntry.Name())
		projName := DecodePath(projEntry.Name())

		files, err := filepath.Glob(filepath.Join(projPath, "*.jsonl"))
		if err != nil {
			continue
		}

		for _, f := range files {
			s, err := ParseSessionFile(f)
			if err != nil || s.MessageCount == 0 {
				continue
			}
			s.Project = projName
			sessions = append(sessions, s)
		}
	}

	return sessions, nil
}

// DiscoverTodaySessions returns only sessions with activity today.
func DiscoverTodaySessions(claudeDir string) ([]*SessionSummary, error) {
	all, err := DiscoverSessions(claudeDir)
	if err != nil {
		return nil, err
	}

	today := time.Now().Truncate(24 * time.Hour)
	var result []*SessionSummary
	for _, s := range all {
		if s.LastActivity.After(today) || s.StartTime.After(today) {
			result = append(result, s)
		}
	}
	return result, nil
}

// ParseSessionDetail returns the full message timeline for a session.
func ParseSessionDetail(path string) ([]SessionMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var messages []SessionMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 2*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry jsonlLine
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		switch entry.Type {
		case "user":
			msg := SessionMessage{
				Type:      "user",
				Timestamp: entry.Timestamp,
				Role:      "user",
			}
			if entry.Message != nil {
				msg.Text = extractText(entry.Message.Content)
			}
			if msg.Text != "" {
				messages = append(messages, msg)
			}

		case "assistant":
			if entry.Message == nil {
				continue
			}
			msg := SessionMessage{
				Type:      "assistant",
				Timestamp: entry.Timestamp,
				Role:      "assistant",
				Model:     entry.Message.Model,
			}
			if entry.Message.Usage != nil {
				msg.TokensIn = entry.Message.Usage.InputTokens
				msg.TokensOut = entry.Message.Usage.OutputTokens
				msg.CacheRead = entry.Message.Usage.CacheReadInputTokens
			}
			msg.Text = extractText(entry.Message.Content)
			msg.ToolUse = extractToolUse(entry.Message.Content)
			messages = append(messages, msg)
		}
	}

	return messages, scanner.Err()
}

// FindSessionFile finds the JSONL file for a session ID.
func FindSessionFile(claudeDir, sessionID string) string {
	projectsDir := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(projectsDir, e.Name(), sessionID+".jsonl")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func extractText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}

	// Try as string first
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}

	// Try as array of content blocks
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

func extractToolUse(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}
	var blocks []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(content, &blocks); err == nil {
		var tools []string
		for _, b := range blocks {
			if b.Type == "tool_use" && b.Name != "" {
				tools = append(tools, b.Name)
			}
		}
		return strings.Join(tools, ", ")
	}
	return ""
}

var xmlTagRe = regexp.MustCompile(`<[^>]+>`)

// stripXMLTags removes XML/HTML tags and their content when the tag wraps the entire
// line (like <ide_opened_file>...</ide_opened_file>), then removes any remaining tags.
func stripXMLTags(s string) string {
	// Remove full tagged lines (e.g. "<ide_opened_file>...text...</ide_opened_file>")
	s = regexp.MustCompile(`(?m)^<\w[^>]*>.*?</\w+>\s*`).ReplaceAllString(s, "")
	// Remove any remaining tags
	s = xmlTagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// DecodePath converts an encoded project directory name back to a filesystem path.
// E.g., "c--Users-fabio-repov2" -> "c:\Users\fabio\repov2" (Windows)
// The encoding replaces : with - and path separators with -
func DecodePath(encoded string) string {
	if len(encoded) < 2 {
		return encoded
	}

	// Handle drive letter: "c--" means "c:\"
	if len(encoded) >= 3 && encoded[1] == '-' && encoded[2] == '-' {
		drive := string(encoded[0])
		rest := encoded[3:]
		parts := strings.Split(rest, "-")
		sep := string(filepath.Separator)
		return drive + ":" + sep + strings.Join(parts, sep)
	}

	// Unix-style: just replace - with /
	return "/" + strings.ReplaceAll(encoded, "-", "/")
}
