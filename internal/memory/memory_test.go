package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"heading", "# Hello", "<h1>Hello</h1>\n"},
		{"paragraph", "Hello world", "<p>Hello world</p>\n"},
		{"bold", "**bold**", "<p><strong>bold</strong></p>\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("renderMarkdown(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name              string
		content           string
		wantName, wantDesc, wantType, wantBody string
	}{
		{
			name: "full frontmatter",
			content: "---\nname: my-memory\ndescription: a test memory\ntype: feedback\n---\nBody content here.",
			wantName: "my-memory",
			wantDesc: "a test memory",
			wantType: "feedback",
			wantBody: "Body content here.",
		},
		{
			name:     "no frontmatter",
			content:  "Just a body.",
			wantBody: "Just a body.",
		},
		{
			name: "quoted values",
			content: "---\nname: \"quoted name\"\ndescription: 'single quoted'\ntype: user\n---\nBody.",
			wantName: "quoted name",
			wantDesc: "single quoted",
			wantType: "user",
			wantBody: "Body.",
		},
		{
			name:     "unclosed frontmatter",
			content:  "---\nname: test\nno closing",
			wantBody: "---\nname: test\nno closing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, desc, fmType, body := parseFrontmatter(tt.content)
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if desc != tt.wantDesc {
				t.Errorf("description = %q, want %q", desc, tt.wantDesc)
			}
			if fmType != tt.wantType {
				t.Errorf("type = %q, want %q", fmType, tt.wantType)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

func TestDecodePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Users-fabio-Projects-claumon", "/Users/fabio/Projects/claumon"},
		{"c--Users-fabio-repo", "c:" + string(filepath.Separator) + "Users" + string(filepath.Separator) + "fabio" + string(filepath.Separator) + "repo"},
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

func TestSearchMemories(t *testing.T) {
	files := []*MemoryFile{
		{Path: "/a.md", Content: "Go testing patterns", FMName: "testing"},
		{Path: "/b.md", Content: "Python logging", FMName: "logging"},
		{Path: "/c.md", Content: "Go concurrency", Project: "myproject"},
	}

	tests := []struct {
		query string
		want  int
	}{
		{"Go", 2},
		{"python", 1},
		{"myproject", 1},
		{"testing", 1},
		{"nonexistent", 0},
		{"", 3}, // empty query returns all
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := SearchMemories(files, tt.query)
			if len(got) != tt.want {
				t.Errorf("SearchMemories(%q) returned %d results, want %d", tt.query, len(got), tt.want)
			}
		})
	}
}

func TestDiscoverAll(t *testing.T) {
	// Create a minimal claude dir structure
	dir := t.TempDir()

	// Global CLAUDE.md
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Global"), 0644); err != nil {
		t.Fatal(err)
	}

	// Rules dir
	rulesDir := filepath.Join(dir, "rules")
	os.MkdirAll(rulesDir, 0755)
	os.WriteFile(filepath.Join(rulesDir, "rule1.md"), []byte("rule one"), 0644)

	// Project with memory
	projDir := filepath.Join(dir, "projects", "Users-fabio-Projects-test")
	memDir := filepath.Join(projDir, "memory")
	os.MkdirAll(memDir, 0755)
	os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("# Index\n- [note.md](note.md)"), 0644)
	os.WriteFile(filepath.Join(memDir, "note.md"), []byte("---\nname: test note\ntype: feedback\n---\nA note."), 0644)

	files, err := DiscoverAll(dir)
	if err != nil {
		t.Fatalf("DiscoverAll() error: %v", err)
	}

	// Should find: CLAUDE.md, rule1.md, MEMORY.md, note.md
	if len(files) < 4 {
		t.Errorf("DiscoverAll() found %d files, want at least 4", len(files))
	}

	// Check frontmatter was parsed
	for _, f := range files {
		if filepath.Base(f.Path) == "note.md" {
			if f.FMName != "test note" {
				t.Errorf("note.md FMName = %q, want %q", f.FMName, "test note")
			}
			if f.FMType != "feedback" {
				t.Errorf("note.md FMType = %q, want %q", f.FMType, "feedback")
			}
			if f.HTMLContent == "" {
				t.Error("note.md HTMLContent should not be empty")
			}
		}
	}
}
