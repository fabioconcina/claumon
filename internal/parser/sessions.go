package parser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fabioconcina/claumon/internal/memory"
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
	HasFileEdits    bool      `json:"has_file_edits"`
	CacheEfficiency float64   `json:"cache_efficiency"`
	WasteFlags      []string  `json:"waste_flags"`
	ContextLength   int       `json:"context_length"`
	IsRunning       bool      `json:"is_running"`
	IsStuck         bool      `json:"is_stuck"`
	PID             int       `json:"pid,omitempty"`
	IdleMinutes     float64   `json:"idle_minutes,omitempty"`
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
	CacheRead   int       `json:"cache_read,omitempty"`
	CacheCreate int       `json:"cache_create,omitempty"`
	ToolCalls   []ToolCall `json:"tool_calls,omitempty"`
	Thinking    string    `json:"thinking,omitempty"`
}

// ToolCall represents a single tool_use invocation from an assistant message,
// optionally paired with its matching tool_result from the following user message.
type ToolCall struct {
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input,omitempty"`
	Result  string          `json:"result,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
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

// newSessionScanner creates a scanner for reading JSONL session files with a 2MB line limit.
func newSessionScanner(f *os.File) *bufio.Scanner {
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 256*1024), 10*1024*1024) // 10MB: Claude sessions can have very large tool-result lines
	return s
}

func ParseSessionFile(path string) (*SessionSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening session file: %w", err)
	}
	defer f.Close()

	summary := &SessionSummary{
		ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"),
	}

	scanner := newSessionScanner(f)

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
					summary.ContextLength = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
				}
				if !summary.HasFileEdits && hasFileEditTool(entry.Message.Content) {
					summary.HasFileEdits = true
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
	summary.CacheEfficiency = cacheEfficiency(summary)
	summary.WasteFlags = detectWaste(summary)
	// Don't fail the whole session on scanner errors (e.g. lines exceeding buffer).
	// We still have valid data from everything parsed before the error.
	return summary, nil
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
		return "claude-opus-4-7"
	case strings.Contains(model, "haiku"):
		return "claude-haiku-4-5"
	default:
		return "claude-sonnet-4-6"
	}
}

type sessionFile struct {
	path    string
	project string
	modTime time.Time
}

// enumerateSessionFiles walks claudeDir/projects/*/ for *.jsonl and returns
// their paths, decoded project names, and mtimes. Stat failures are skipped silently.
func enumerateSessionFiles(claudeDir string) ([]sessionFile, error) {
	projectsDir := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("reading projects directory: %w", err)
	}

	var files []sessionFile
	for _, projEntry := range entries {
		if !projEntry.IsDir() {
			continue
		}
		projPath := filepath.Join(projectsDir, projEntry.Name())
		projName := memory.DecodePath(projEntry.Name())

		matches, err := filepath.Glob(filepath.Join(projPath, "*.jsonl"))
		if err != nil {
			continue
		}
		for _, f := range matches {
			info, err := os.Stat(f)
			if err != nil {
				continue
			}
			files = append(files, sessionFile{path: f, project: projName, modTime: info.ModTime()})
		}
	}
	return files, nil
}

// parseSessionFiles parses each file and returns the non-empty summaries with project tagged.
func parseSessionFiles(files []sessionFile) []*SessionSummary {
	var sessions []*SessionSummary
	for _, f := range files {
		s, err := ParseSessionFile(f.path)
		if err != nil || s.MessageCount == 0 {
			continue
		}
		s.Project = f.project
		sessions = append(sessions, s)
	}
	return sessions
}

func DiscoverSessions(claudeDir string) ([]*SessionSummary, error) {
	files, err := enumerateSessionFiles(claudeDir)
	if err != nil {
		return nil, err
	}
	return parseSessionFiles(files), nil
}

// DiscoverRecentSessions returns at most limit sessions, sorted by file modification time.
// This avoids parsing all JSONL files by only parsing the most recently modified ones.
func DiscoverRecentSessions(claudeDir string, limit int) ([]*SessionSummary, error) {
	files, err := enumerateSessionFiles(claudeDir)
	if err != nil {
		return nil, err
	}
	// Sort by modification time descending (most recent first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})
	if len(files) > limit {
		files = files[:limit]
	}
	return parseSessionFiles(files), nil
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
		return nil, fmt.Errorf("opening session file: %w", err)
	}
	defer f.Close()

	var messages []SessionMessage
	scanner := newSessionScanner(f)

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
			if entry.Message == nil {
				continue
			}
			// Attach tool_results to the matching tool_use on the most
			// recent preceding assistant message, and drop this user entry
			// if it only carried tool_results (no user-visible text).
			if results := extractToolResults(entry.Message.Content); len(results) > 0 {
				for i := len(messages) - 1; i >= 0; i-- {
					if messages[i].Role != "assistant" {
						continue
					}
					for j := range messages[i].ToolCalls {
						if r, ok := results[messages[i].ToolCalls[j].ID]; ok {
							messages[i].ToolCalls[j].Result = r.Text
							messages[i].ToolCalls[j].IsError = r.IsError
						}
					}
					break
				}
			}
			if hasOnlyToolResults(entry.Message.Content) {
				continue
			}
			msg := SessionMessage{
				Type:      "user",
				Timestamp: entry.Timestamp,
				Role:      "user",
				Text:      extractText(entry.Message.Content),
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
				msg.CacheCreate = entry.Message.Usage.CacheCreationInputTokens
			}
			msg.Text = extractText(entry.Message.Content)
			msg.ToolCalls = extractToolCalls(entry.Message.Content)
			msg.Thinking = extractThinking(entry.Message.Content)
			// Skip empty assistant messages (e.g. thinking-only with no
			// visible content — Claude Code persists only the signature,
			// not the thinking text).
			if msg.Text == "" && len(msg.ToolCalls) == 0 && msg.Thinking == "" {
				continue
			}
			messages = append(messages, msg)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning session detail: %w", err)
	}
	return messages, nil
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

// SessionAggregate holds aggregated token counts and cost across sessions.
type SessionAggregate struct {
	InputTokens       int     `json:"input_tokens"`
	OutputTokens      int     `json:"output_tokens"`
	CacheReadTokens   int     `json:"cache_read_tokens"`
	CacheCreateTokens int     `json:"cache_create_tokens"`
	CostUSD           float64 `json:"cost_usd"`
	SessionCount      int     `json:"session_count"`
	MessageCount      int     `json:"message_count"`
}

// AggregateSessions sums token counts, costs, and session/message counts.
func AggregateSessions(sessions []*SessionSummary) SessionAggregate {
	var agg SessionAggregate
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
	return agg
}

// contentBlock represents a single block in a Claude message content array.
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Name      string          `json:"name,omitempty"`
	ID        string          `json:"id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// parseContentBlocks parses message content as either a string or array of blocks.
// Returns nil if content is empty or unparseable.
func parseContentBlocks(content json.RawMessage) []contentBlock {
	if len(content) == 0 {
		return nil
	}

	// Try as string first
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return []contentBlock{{Type: "text", Text: s}}
	}

	// Try as array of content blocks
	var blocks []contentBlock
	if err := json.Unmarshal(content, &blocks); err == nil {
		return blocks
	}
	return nil
}

func extractText(content json.RawMessage) string {
	blocks := parseContentBlocks(content)
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func extractThinking(content json.RawMessage) string {
	blocks := parseContentBlocks(content)
	var parts []string
	for _, b := range blocks {
		if b.Type == "thinking" && b.Thinking != "" {
			parts = append(parts, b.Thinking)
		}
	}
	return strings.Join(parts, "\n")
}

func extractToolCalls(content json.RawMessage) []ToolCall {
	blocks := parseContentBlocks(content)
	var calls []ToolCall
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name != "" {
			calls = append(calls, ToolCall{
				ID:    b.ID,
				Name:  b.Name,
				Input: b.Input,
			})
		}
	}
	return calls
}

// toolResult is an intermediate struct used when pairing tool_result blocks
// from user messages back to their originating tool_use calls.
type toolResult struct {
	Text    string
	IsError bool
}

// extractToolResults returns tool_result blocks keyed by their tool_use_id.
// Result content may be a plain string or an array of text blocks — both are
// normalized to a single string and truncated to avoid bloating the API payload.
func extractToolResults(content json.RawMessage) map[string]toolResult {
	blocks := parseContentBlocks(content)
	if len(blocks) == 0 {
		return nil
	}
	out := map[string]toolResult{}
	for _, b := range blocks {
		if b.Type != "tool_result" || b.ToolUseID == "" {
			continue
		}
		text := normalizeResultContent(b.Content)
		if len(text) > 4096 {
			text = text[:4096] + "\n\n… (truncated)"
		}
		out[b.ToolUseID] = toolResult{Text: text, IsError: b.IsError}
	}
	return out
}

// normalizeResultContent handles the two shapes tool_result.content can take:
// a bare string, or an array of {type:"text", text:"..."} blocks.
func normalizeResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// hasOnlyToolResults reports whether a content payload contains tool_result
// blocks and nothing else worth displaying as a user message bubble.
func hasOnlyToolResults(content json.RawMessage) bool {
	blocks := parseContentBlocks(content)
	if len(blocks) == 0 {
		return false
	}
	sawToolResult := false
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			sawToolResult = true
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				return false
			}
		default:
			return false
		}
	}
	return sawToolResult
}

var (
	xmlTagRe     = regexp.MustCompile(`<[^>]+>`)
	fullTagLineRe = regexp.MustCompile(`(?m)^<\w[^>]*>.*?</\w+>\s*`)
)

// stripXMLTags removes XML/HTML tags and their content when the tag wraps the entire
// line (like <ide_opened_file>...</ide_opened_file>), then removes any remaining tags.
func stripXMLTags(s string) string {
	s = fullTagLineRe.ReplaceAllString(s, "")
	s = xmlTagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// fileEditTools are tool names that indicate the session produced file changes.
var fileEditTools = map[string]bool{
	"Write": true, "Edit": true, "NotebookEdit": true,
	"write": true, "edit": true,
}

// hasFileEditTool checks if a message's content blocks contain a file-editing tool_use.
func hasFileEditTool(content json.RawMessage) bool {
	for _, b := range parseContentBlocks(content) {
		if b.Type == "tool_use" && fileEditTools[b.Name] {
			return true
		}
	}
	return false
}

// cacheEfficiency returns the fraction of input context served from existing cache.
// Computed as cache_read / (input + cache_read + cache_create).
// Returns 0 if there is no input context.
func cacheEfficiency(s *SessionSummary) float64 {
	total := s.InputTokens + s.CacheReadTokens + s.CacheCreateTokens
	if total == 0 {
		return 0
	}
	ratio := float64(s.CacheReadTokens) / float64(total)
	return math.Round(ratio*1000) / 1000
}

const (
	wasteMinTokens      = 50_000
	wasteMinMessages    = 10
	wasteLowEfficiency  = 0.5
)

// detectWaste flags sessions with low cache efficiency or high token
// usage without any file edits.
func detectWaste(s *SessionSummary) []string {
	totalTokens := s.InputTokens + s.OutputTokens + s.CacheReadTokens + s.CacheCreateTokens
	var flags []string

	if s.CacheEfficiency <= wasteLowEfficiency && totalTokens >= wasteMinTokens {
		flags = append(flags, "low_cache_efficiency")
	}

	if !s.HasFileEdits && totalTokens >= wasteMinTokens && s.MessageCount >= wasteMinMessages {
		flags = append(flags, "no_file_edits")
	}

	if flags == nil {
		flags = []string{}
	}
	return flags
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

