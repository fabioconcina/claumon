package parser

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/fabioconcina/claumon/internal/pricing"
)

func TestMain(m *testing.M) {
	// Initialize pricing table from embedded data for all tests.
	SetPricingTable(pricing.Load(nil))
	os.Exit(m.Run())
}

func TestNormalizeModel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"claude-opus-4-6", "claude-opus-4-6"},
		{"claude-opus-4-6-20250514", "claude-opus-4-6"},
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"claude-sonnet-4-6-20250514", "claude-sonnet-4-6"},
		{"claude-sonnet-4-5", "claude-sonnet-4-5"},
		{"claude-sonnet-4-0", "claude-sonnet-4-0"},
		{"claude-haiku-4-5", "claude-haiku-4-5"},
		{"claude-haiku-4-5-20251001", "claude-haiku-4-5"},
		{"claude-haiku-3-5", "claude-haiku-3-5"},
		// Family-based fallback
		{"some-opus-model", "claude-opus-4-6"},
		{"some-haiku-model", "claude-haiku-4-5"},
		{"unknown-model", "claude-sonnet-4-6"},
		{"", "claude-sonnet-4-6"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeModel(tt.input)
			if got != tt.want {
				t.Errorf("normalizeModel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEstimateCost(t *testing.T) {
	tests := []struct {
		name    string
		summary SessionSummary
		want    float64
	}{
		{
			name: "opus pricing",
			summary: SessionSummary{
				Model:        "claude-opus-4-6",
				InputTokens:  1_000_000,
				OutputTokens: 1_000_000,
			},
			want: 5.0 + 25.0, // $5/M input + $25/M output
		},
		{
			name: "sonnet pricing with cache",
			summary: SessionSummary{
				Model:             "claude-sonnet-4-6",
				InputTokens:       100_000,
				OutputTokens:      50_000,
				CacheReadTokens:   200_000,
				CacheCreateTokens: 10_000,
			},
			// 0.1M * $3 + 0.05M * $15 + 0.2M * $0.30 + 0.01M * $3.75
			want: 0.3 + 0.75 + 0.06 + 0.0375,
		},
		{
			name: "zero tokens",
			summary: SessionSummary{
				Model: "claude-sonnet-4-6",
			},
			want: 0,
		},
		{
			name: "unknown model defaults to sonnet",
			summary: SessionSummary{
				Model:       "unknown-model-v99",
				InputTokens: 1_000_000,
			},
			want: 3.0, // sonnet input pricing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateCost(&tt.summary)
			if math.Abs(got-tt.want) > 0.0001 {
				t.Errorf("estimateCost() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestDecodePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Users-fabio-Projects-tokukan", "/Users/fabio/Projects/tokukan"},
		{"c--Users-fabio-repov2", "c:" + string(filepath.Separator) + "Users" + string(filepath.Separator) + "fabio" + string(filepath.Separator) + "repov2"},
		{"a", "a"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := DecodePath(tt.input)
			if got != tt.want {
				t.Errorf("DecodePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"this is a long string", 10, "this is a…"},
		{"newline\nin string", 80, "newline in string"},
		{"  spaces  ", 80, "spaces"},
		{"", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestParseSessionFile(t *testing.T) {
	// Create a temp JSONL session file
	dir := t.TempDir()
	path := filepath.Join(dir, "test-session-123.jsonl")

	content := `{"type":"user","timestamp":"2026-03-20T10:00:00Z","sessionId":"test-session-123","cwd":"/tmp/project","message":{"role":"user","content":"Hello"}}
{"type":"assistant","timestamp":"2026-03-20T10:00:01Z","message":{"model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"Hi there!"}],"usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":200,"cache_creation_input_tokens":0}}}
{"type":"ai-title","timestamp":"2026-03-20T10:00:02Z","title":"Test Conversation"}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := ParseSessionFile(path)
	if err != nil {
		t.Fatalf("ParseSessionFile() error: %v", err)
	}

	if s.ID != "test-session-123" {
		t.Errorf("ID = %q, want %q", s.ID, "test-session-123")
	}
	if s.Title != "Test Conversation" {
		t.Errorf("Title = %q, want %q", s.Title, "Test Conversation")
	}
	if s.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q", s.Model, "claude-sonnet-4-6")
	}
	if s.CWD != "/tmp/project" {
		t.Errorf("CWD = %q, want %q", s.CWD, "/tmp/project")
	}
	if s.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", s.MessageCount)
	}
	if s.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", s.InputTokens)
	}
	if s.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", s.OutputTokens)
	}
	if s.CacheReadTokens != 200 {
		t.Errorf("CacheReadTokens = %d, want 200", s.CacheReadTokens)
	}
	if s.EstimatedCostUSD <= 0 {
		t.Error("EstimatedCostUSD should be > 0")
	}
}

func TestParseSessionDetail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "detail-test.jsonl")

	content := `{"type":"user","timestamp":"2026-03-20T10:00:00Z","message":{"role":"user","content":"What is 2+2?"}}
{"type":"assistant","timestamp":"2026-03-20T10:00:01Z","message":{"model":"claude-sonnet-4-6","role":"assistant","content":[{"type":"text","text":"4"},{"type":"tool_use","name":"Calculator"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	msgs, err := ParseSessionDetail(path)
	if err != nil {
		t.Fatalf("ParseSessionDetail() error: %v", err)
	}

	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}

	if msgs[0].Role != "user" || msgs[0].Text != "What is 2+2?" {
		t.Errorf("msg[0] = %+v, want user message", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Text != "4" {
		t.Errorf("msg[1] text = %q, want %q", msgs[1].Text, "4")
	}
	if msgs[1].ToolUse != "Calculator" {
		t.Errorf("msg[1] tool_use = %q, want %q", msgs[1].ToolUse, "Calculator")
	}
	if msgs[1].TokensIn != 10 || msgs[1].TokensOut != 5 {
		t.Errorf("msg[1] tokens = in:%d out:%d, want in:10 out:5", msgs[1].TokensIn, msgs[1].TokensOut)
	}
}

func TestTotalTokens(t *testing.T) {
	s := SessionSummary{
		InputTokens:       100,
		OutputTokens:      200,
		CacheReadTokens:   300,
		CacheCreateTokens: 400,
	}
	if got := s.TotalTokens(); got != 1000 {
		t.Errorf("TotalTokens() = %d, want 1000", got)
	}
}
