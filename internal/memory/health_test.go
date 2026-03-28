package memory

import (
	"testing"
	"time"
)

func TestScoreHealthEmpty(t *testing.T) {
	report := ScoreHealth(nil)
	if len(report.Scores) != 0 {
		t.Errorf("expected 0 scores, got %d", len(report.Scores))
	}
	if report.AverageScore != 0 {
		t.Errorf("expected average 0, got %d", report.AverageScore)
	}
}

func TestScoreHealthSkipsNonMemoryFiles(t *testing.T) {
	files := []*MemoryFile{
		{Path: "/p/CLAUDE.md", Category: "claude-md", Content: "# Config"},
		{Path: "/p/rules/foo.md", Category: "rules", Content: "rule"},
		{Path: "/p/memory/MEMORY.md", Category: "auto-memory", Content: "- [a](a.md)"},
	}
	report := ScoreHealth(files)
	if len(report.Scores) != 0 {
		t.Errorf("expected 0 scored files (only memory-file category), got %d", len(report.Scores))
	}
}

func TestScoreFreshness(t *testing.T) {
	tests := []struct {
		name    string
		daysAgo int
		fmType  string
		wantMin int
		wantMax int
	}{
		{"today project", 0, "project", 100, 100},
		{"one week project", 7, "project", 85, 95},
		{"one month project", 30, "project", 60, 70},
		{"two months project", 60, "project", 25, 40},
		{"three months project", 90, "project", 0, 0},
		{"six months project", 180, "project", 0, 0},
		// Durable types always score 100 regardless of age
		{"old feedback", 180, "feedback", 100, 100},
		{"old user", 365, "user", 100, 100},
		{"old reference", 90, "reference", 100, 100},
		// Untyped memories decay like project
		{"old untyped", 90, "", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modTime := time.Now().Add(-time.Duration(tt.daysAgo) * 24 * time.Hour).Unix()
			score := scoreFreshness(modTime, tt.fmType)
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("scoreFreshness(%d days ago, %q) = %d, want [%d, %d]", tt.daysAgo, tt.fmType, score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestScoreStructure(t *testing.T) {
	tests := []struct {
		name string
		file *MemoryFile
		want int
	}{
		{
			"no frontmatter",
			&MemoryFile{},
			0,
		},
		{
			"full frontmatter",
			&MemoryFile{FMName: "test", FMDescription: "desc", FMType: "user"},
			100,
		},
		{
			"name only",
			&MemoryFile{FMName: "test"},
			55, // 30 (has FM) + 25 (name)
		},
		{
			"type only",
			&MemoryFile{FMType: "feedback"},
			50, // 30 (has FM) + 20 (valid type)
		},
		{
			"invalid type",
			&MemoryFile{FMType: "unknown"},
			30, // 30 (has FM) + 0 (invalid type)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreStructure(tt.file)
			if score != tt.want {
				t.Errorf("scoreStructure() = %d, want %d", score, tt.want)
			}
		})
	}
}

func TestScoreSpecificity(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		content string
		wantMin int
	}{
		{"empty", "", "", 0},
		{"vague prose no verbs", "Some general thoughts about things", "Some general thoughts about things", 0},
		{
			"actionable no code",
			"Always prefer one bundled PR over many small ones for refactors in this repo",
			"Always prefer one bundled PR over many small ones for refactors in this repo",
			20,
		},
		{
			"with code block",
			"Always run this before deploying:\n```\ngo build ./...\n```",
			"Always run this before deploying:\n```\ngo build ./...\n```",
			20,
		},
		{
			"with why/how structure",
			"Never mock the database in tests.\n\n**Why:** mocks diverged from prod last quarter.\n\n**How to apply:** when writing integration tests, always use a real test database.",
			"---\ntype: feedback\n---\nNever mock the database in tests.\n\n**Why:** mocks diverged from prod last quarter.\n\n**How to apply:** when writing integration tests, always use a real test database.",
			50,
		},
		{
			"scoped rule",
			"When deploying alertpaca, always bump the version before building",
			"When deploying alertpaca, always bump the version before building",
			20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreSpecificity(tt.body, tt.content)
			if score < tt.wantMin {
				t.Errorf("scoreSpecificity() = %d, want >= %d", score, tt.wantMin)
			}
		})
	}
}

func TestScoreConnectedness(t *testing.T) {
	indexed := map[string]bool{"/mem/a.md": true}

	tests := []struct {
		name string
		path string
		want int
	}{
		{"indexed", "/mem/a.md", 100},
		{"not indexed", "/mem/b.md", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &MemoryFile{Path: tt.path, Category: "memory-file"}
			score := scoreConnectedness(f, indexed)
			if score != tt.want {
				t.Errorf("scoreConnectedness() = %d, want %d", score, tt.want)
			}
		})
	}
}

func TestLetterGrade(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{100, "A"}, {85, "A"}, {84, "B"}, {70, "B"},
		{69, "C"}, {50, "C"}, {49, "D"}, {30, "D"},
		{29, "F"}, {0, "F"},
	}

	for _, tt := range tests {
		got := letterGrade(tt.score)
		if got != tt.want {
			t.Errorf("letterGrade(%d) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestScoreHealthFullPipeline(t *testing.T) {
	now := time.Now().Unix()

	files := []*MemoryFile{
		{
			Path:     "/mem/MEMORY.md",
			Category: "auto-memory",
			Content:  "- [good](good.md)\n- [bad](bad.md)",
			Project:  "proj",
			ModTime:  now,
		},
		{
			Path:          "/mem/good.md",
			Category:      "memory-file",
			Content:       "---\nname: Good Memory\ndescription: A well-structured memory\ntype: feedback\n---\nUse `go test -race` always.\n\n**Why:** Race conditions are subtle.\n\n**How to apply:** Run on every PR.",
			FMName:        "Good Memory",
			FMDescription: "A well-structured memory",
			FMType:        "feedback",
			Project:       "proj",
			ModTime:       now,
		},
		{
			Path:     "/mem/bad.md",
			Category: "memory-file",
			Content:  "stuff",
			Project:  "proj",
			ModTime:  now - 100*86400, // 100 days old
		},
	}

	report := ScoreHealth(files)

	if len(report.Scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(report.Scores))
	}

	// Scores are sorted worst-first
	worst := report.Scores[0]
	best := report.Scores[1]

	if worst.Path != "/mem/bad.md" {
		t.Errorf("expected worst file to be bad.md, got %s", worst.Path)
	}
	if best.Path != "/mem/good.md" {
		t.Errorf("expected best file to be good.md, got %s", best.Path)
	}

	if best.Overall <= worst.Overall {
		t.Errorf("good file (%d) should score higher than bad file (%d)", best.Overall, worst.Overall)
	}

	if len(worst.Suggestions) == 0 {
		t.Error("bad file should have suggestions")
	}

	if report.AverageScore <= 0 {
		t.Error("average score should be positive")
	}
}

func TestBuildSuggestions(t *testing.T) {
	indexed := map[string]bool{}
	idxPaths := map[string]string{"proj": "/mem/MEMORY.md"}

	f := &MemoryFile{
		Path:     "/mem/test.md",
		Category: "memory-file",
		Content:  "short",
		Project:  "proj",
		FMType:   "feedback",
	}

	suggestions := buildSuggestions(f, "short", 10, 0, 10, 0, indexed, idxPaths)

	// Should suggest: stale, frontmatter, not linked, why section
	wantKeywords := []string{"Getting stale", "frontmatter", "MEMORY.md", "Why"}
	for _, kw := range wantKeywords {
		found := false
		for _, s := range suggestions {
			if contains(s, kw) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected suggestion containing %q, got: %v", kw, suggestions)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsLower(s, substr))
}

func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
