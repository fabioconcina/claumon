package memory

import (
	"strings"
	"testing"
)

func TestFindConsolidationEmpty(t *testing.T) {
	report := FindConsolidation(nil)
	if len(report.Groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(report.Groups))
	}

	report = FindConsolidation([]*MemoryFile{})
	if len(report.Groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(report.Groups))
	}
}

func TestFindConsolidationSameProjectExcluded(t *testing.T) {
	files := []*MemoryFile{
		{Path: "/proj/memory/a.md", Project: "/proj", Category: "memory-file",
			Content: "Deploy to server fabius@192.168.1.100 using scp to ~/.local/bin/mytool"},
		{Path: "/proj/memory/b.md", Project: "/proj", Category: "memory-file",
			Content: "Deploy to server fabius@192.168.1.100 using scp to ~/.local/bin/mytool"},
	}
	report := FindConsolidation(files)
	if len(report.Groups) != 0 {
		t.Errorf("same-project files should not be grouped, got %d groups", len(report.Groups))
	}
}

func TestFindConsolidationSharedEntities(t *testing.T) {
	files := []*MemoryFile{
		{Path: "/projA/memory/deploy.md", Project: "/projA", Category: "memory-file",
			Content: "Deploy binary to fabius@192.168.1.100 at ~/.local/bin/alertpaca"},
		{Path: "/projB/memory/server.md", Project: "/projB", Category: "memory-file",
			Content: "Server access fabius@192.168.1.100 has ~/.local/bin/arpdvark and ~/.local/bin/alertpaca"},
	}
	report := FindConsolidation(files)
	if len(report.Groups) == 0 {
		t.Fatal("expected at least 1 group for files sharing SSH ref and binary path")
	}
	if len(report.Groups[0].Files) != 2 {
		t.Errorf("expected 2 files in group, got %d", len(report.Groups[0].Files))
	}
}

func TestFindConsolidationSharedContent(t *testing.T) {
	content := "cross compile cargo zigbuild release target linux gnu deploy scp binary server"
	files := []*MemoryFile{
		{Path: "/projA/memory/build.md", Project: "/projA", Category: "memory-file",
			Content: "How to " + content + " for alertpaca", FMType: "reference"},
		{Path: "/projB/memory/build.md", Project: "/projB", Category: "memory-file",
			Content: "Steps to " + content + " for pingolin", FMType: "reference"},
	}
	report := FindConsolidation(files)
	if len(report.Groups) == 0 {
		t.Fatal("expected at least 1 group for files with overlapping content and same FMType")
	}
}

func TestFindConsolidationNoOverlap(t *testing.T) {
	files := []*MemoryFile{
		{Path: "/projA/memory/auth.md", Project: "/projA", Category: "memory-file",
			Content: "Authentication uses JWT tokens with RSA256 signing"},
		{Path: "/projB/memory/colors.md", Project: "/projB", Category: "memory-file",
			Content: "The theme uses solarized dark palette with custom accent"},
	}
	report := FindConsolidation(files)
	if len(report.Groups) != 0 {
		t.Errorf("expected 0 groups for unrelated files, got %d", len(report.Groups))
	}
}

func TestFindConsolidationSkipsNonMemoryFiles(t *testing.T) {
	files := []*MemoryFile{
		{Path: "/projA/CLAUDE.md", Project: "/projA", Category: "claude-md",
			Content: "Deploy to fabius@192.168.1.100 using scp"},
		{Path: "/projB/memory/deploy.md", Project: "/projB", Category: "memory-file",
			Content: "Deploy to fabius@192.168.1.100 using scp"},
	}
	report := FindConsolidation(files)
	if len(report.Groups) != 0 {
		t.Errorf("expected 0 groups when one file is claude-md, got %d", len(report.Groups))
	}
}

func TestJaccard(t *testing.T) {
	tests := []struct {
		a, b map[string]bool
		want float64
	}{
		{nil, nil, 0},
		{map[string]bool{"a": true}, map[string]bool{}, 0},
		{map[string]bool{"a": true}, map[string]bool{"a": true}, 1.0},
		{map[string]bool{"a": true, "b": true}, map[string]bool{"b": true, "c": true}, 1.0 / 3.0},
	}
	for i, tt := range tests {
		got := jaccard(tt.a, tt.b)
		if got < tt.want-0.001 || got > tt.want+0.001 {
			t.Errorf("test %d: jaccard() = %f, want %f", i, got, tt.want)
		}
	}
}

func TestTokenize(t *testing.T) {
	words := tokenize("The quick brown fox is a very fast animal")
	for _, w := range words {
		if stopWords[w] {
			t.Errorf("tokenize should filter stop word %q", w)
		}
	}
	if len(words) == 0 {
		t.Error("tokenize should return non-empty for non-trivial input")
	}
	// Should contain "quick", "brown", "fox", "fast", "animal"
	joined := strings.Join(words, " ")
	for _, want := range []string{"quick", "brown", "fox", "fast", "animal"} {
		if !strings.Contains(joined, want) {
			t.Errorf("tokenize should include %q", want)
		}
	}
}

func TestBuildPrompt(t *testing.T) {
	g := ConsolidationGroup{
		Files: []*MemoryFile{
			{Path: "/projA/memory/server.md"},
			{Path: "/projB/memory/deploy.md"},
		},
		SharedKeywords: []string{"ssh:fabius@192.168.1.100", "deploy server"},
	}
	prompt := buildPrompt(g)
	if !strings.Contains(prompt, "/projA/memory/server.md") {
		t.Error("prompt should contain file paths")
	}
	if !strings.Contains(prompt, "Consolidate") {
		t.Error("prompt should contain Consolidate instruction")
	}
	if !strings.Contains(prompt, "ssh:fabius@192.168.1.100") {
		t.Error("prompt should contain shared keywords")
	}
}
