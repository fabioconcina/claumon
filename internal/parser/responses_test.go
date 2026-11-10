package parser

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// dupSession has one response (msg_1) logged as two content-block lines that
// repeat the same usage, one response (msg_2) whose last line carries the
// final usage, and one legacy line with no message ID.
const dupSession = `{"type":"user","timestamp":"2026-03-20T10:00:00Z","message":{"role":"user","content":"hi"}}
{"type":"assistant","timestamp":"2026-03-20T10:00:01Z","message":{"id":"msg_1","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"thinking","thinking":"..."}],"usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":1000,"cache_creation_input_tokens":10}}}
{"type":"assistant","timestamp":"2026-03-20T10:00:02Z","message":{"id":"msg_1","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"tool_use","name":"Read"}],"usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":1000,"cache_creation_input_tokens":10}}}
{"type":"assistant","timestamp":"2026-03-20T10:05:00Z","message":{"id":"msg_2","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"a"}],"usage":{"input_tokens":5,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
{"type":"assistant","timestamp":"2026-03-20T10:05:01Z","message":{"id":"msg_2","model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"b"}],"usage":{"input_tokens":5,"output_tokens":20,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
{"type":"assistant","timestamp":"2026-03-20T10:06:00Z","message":{"model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"c"}],"usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
`

// Deduplicated totals: msg_1 once + msg_2's last line + the ID-less line.
const (
	dupIn          = 100 + 5 + 1
	dupOut         = 50 + 20 + 1
	dupCacheRead   = 1000
	dupCacheCreate = 10
)

func writeDupSession(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dup.jsonl")
	if err := os.WriteFile(path, []byte(dupSession), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseSessionFileDedupesResponses(t *testing.T) {
	s, err := ParseSessionFile(writeDupSession(t))
	if err != nil {
		t.Fatal(err)
	}
	if s.InputTokens != dupIn || s.OutputTokens != dupOut ||
		s.CacheReadTokens != dupCacheRead || s.CacheCreateTokens != dupCacheCreate {
		t.Errorf("tokens = in:%d out:%d cr:%d cc:%d, want in:%d out:%d cr:%d cc:%d",
			s.InputTokens, s.OutputTokens, s.CacheReadTokens, s.CacheCreateTokens,
			dupIn, dupOut, dupCacheRead, dupCacheCreate)
	}
	// Message count is per line, unchanged by this dedupe.
	if s.MessageCount != 6 {
		t.Errorf("MessageCount = %d, want 6", s.MessageCount)
	}
	// Context length still reflects the last assistant line.
	if s.ContextLength != 1 {
		t.Errorf("ContextLength = %d, want 1", s.ContextLength)
	}
}

func TestParseSessionFileDailyDedupesResponses(t *testing.T) {
	buckets, err := ParseSessionFileDaily(writeDupSession(t))
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC).Local().Format("2006-01-02")
	b := buckets[date]
	if b == nil {
		t.Fatalf("no bucket for %s: %v", date, buckets)
	}
	if b.InputTokens != dupIn || b.OutputTokens != dupOut ||
		b.CacheReadTokens != dupCacheRead || b.CacheCreateTokens != dupCacheCreate {
		t.Errorf("bucket tokens = %+v", b)
	}
	want := math.Round(costFor("claude-sonnet-4-6", dupIn, dupOut, dupCacheRead, dupCacheCreate, 0)*10000) / 10000
	if math.Abs(b.CostUSD-want) > 1e-9 {
		t.Errorf("CostUSD = %v, want %v", b.CostUSD, want)
	}
}

func TestParseSessionFileHourlyDedupesResponses(t *testing.T) {
	buckets, err := parseSessionFileHourly(writeDupSession(t))
	if err != nil {
		t.Fatal(err)
	}
	var total int
	for _, hours := range buckets {
		for _, n := range hours {
			total += n
		}
	}
	if want := dupIn + dupOut + dupCacheRead + dupCacheCreate; total != want {
		t.Errorf("hourly total = %d, want %d", total, want)
	}
}

// cacheTTLSession has one Opus 5.5 response that writes 1M cache tokens,
// 600K of them with the 1-hour lifetime, logged as Claude Code does.
const cacheTTLSession = `{"type":"assistant","timestamp":"2026-11-10T10:00:00Z","message":{"id":"msg_1","model":"claude-opus-5-5","role":"assistant","content":[{"type":"text","text":"a"}],"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":1000000,"cache_creation":{"ephemeral_5m_input_tokens":400000,"ephemeral_1h_input_tokens":600000}}}}
`

func TestCacheWritesPricedByLifetime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ttl.jsonl")
	if err := os.WriteFile(path, []byte(cacheTTLSession), 0644); err != nil {
		t.Fatal(err)
	}
	// Opus 5.5: 0.4M at $5 (5-minute) + 0.6M at $8 (1-hour).
	const want = 0.4*5 + 0.6*8

	s, err := ParseSessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.CacheCreateTokens != 1_000_000 {
		t.Errorf("CacheCreateTokens = %d, want 1000000", s.CacheCreateTokens)
	}
	if math.Abs(s.EstimatedCostUSD-want) > 1e-9 {
		t.Errorf("session cost = %v, want %v", s.EstimatedCostUSD, want)
	}

	buckets, err := ParseSessionFileDaily(path)
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC).Local().Format("2006-01-02")
	if b := buckets[date]; b == nil || math.Abs(b.CostUSD-want) > 1e-9 {
		t.Errorf("daily bucket = %+v, want cost %v", b, want)
	}
}

func TestUsageCacheCreate1h(t *testing.T) {
	split := func(h int) *struct {
		Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
	} {
		return &struct {
			Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
		}{h}
	}
	tests := []struct {
		name string
		u    usage
		want int
	}{
		{"no split logged", usage{CacheCreationInputTokens: 100}, 0},
		{"split logged", usage{CacheCreationInputTokens: 100, CacheCreation: split(60)}, 60},
		{"split above total is capped", usage{CacheCreationInputTokens: 100, CacheCreation: split(150)}, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.cacheCreate1h(); got != tt.want {
				t.Errorf("cacheCreate1h() = %d, want %d", got, tt.want)
			}
		})
	}
}
