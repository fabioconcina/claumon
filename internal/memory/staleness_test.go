package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractLinks(t *testing.T) {
	content := `# Index
- [User prefs](user_prefs.md) — user preferences
- [Feedback](feedback_testing.md) — testing feedback
- Not a link
- [Readme](README.md) — readme
`
	links := extractMarkdownLinks(content)
	if len(links) != 3 {
		t.Fatalf("extractLinks() found %d links, want 3", len(links))
	}

	expected := []string{"user_prefs.md", "feedback_testing.md", "README.md"}
	for i, want := range expected {
		if links[i] != want {
			t.Errorf("link[%d] = %q, want %q", i, links[i], want)
		}
	}
}

func TestExtractLinksNoLinks(t *testing.T) {
	links := extractMarkdownLinks("No links here at all.")
	if len(links) != 0 {
		t.Errorf("expected 0 links, got %d", len(links))
	}
}

func TestCheckStalenessNoAlerts(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	os.MkdirAll(memDir, 0755)

	notePath := filepath.Join(memDir, "note.md")
	os.WriteFile(notePath, []byte("a note"), 0644)

	indexPath := filepath.Join(memDir, "MEMORY.md")
	os.WriteFile(indexPath, []byte("- [note](note.md)"), 0644)

	files := []*MemoryFile{
		{Path: indexPath, Category: "auto-memory", Content: "- [note](note.md)", Project: "proj"},
		{Path: notePath, Category: "memory-file", Content: "a note", Project: "proj"},
	}

	report := CheckStaleness(files)
	if len(report.Alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d: %+v", len(report.Alerts), report.Alerts)
	}
}

func TestCheckStalenessBrokenLink(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	os.MkdirAll(memDir, 0755)

	indexPath := filepath.Join(memDir, "MEMORY.md")
	os.WriteFile(indexPath, []byte("- [gone](deleted.md)"), 0644)

	files := []*MemoryFile{
		{Path: indexPath, Category: "auto-memory", Content: "- [gone](deleted.md)", Project: "proj"},
	}

	report := CheckStaleness(files)

	var found bool
	for _, a := range report.Alerts {
		if a.Type == "broken-link" {
			found = true
		}
	}
	if !found {
		t.Error("expected a broken-link alert")
	}
}

func TestCheckStalenessOrphanedFile(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	os.MkdirAll(memDir, 0755)

	indexPath := filepath.Join(memDir, "MEMORY.md")
	os.WriteFile(indexPath, []byte("# Index\nNothing here"), 0644)

	orphanPath := filepath.Join(memDir, "orphan.md")
	os.WriteFile(orphanPath, []byte("I'm alone"), 0644)

	files := []*MemoryFile{
		{Path: indexPath, Category: "auto-memory", Content: "# Index\nNothing here", Project: "proj"},
		{Path: orphanPath, Category: "memory-file", Content: "I'm alone", Project: "proj"},
	}

	report := CheckStaleness(files)

	var found bool
	for _, a := range report.Alerts {
		if a.Type == "orphaned-file" {
			found = true
		}
	}
	if !found {
		t.Error("expected an orphaned-file alert")
	}
}

func TestCheckStalenessIndexMismatch(t *testing.T) {
	files := []*MemoryFile{
		{Path: "/mem/MEMORY.md", Category: "auto-memory", Content: "- [a](a.md)\n- [b](b.md)", Project: "proj"},
		{Path: "/mem/a.md", Category: "memory-file", Content: "a", Project: "proj"},
		// b.md is listed in index but exists as a known file, c.md is an extra memory-file
		{Path: "/mem/b.md", Category: "memory-file", Content: "b", Project: "proj"},
		{Path: "/mem/c.md", Category: "memory-file", Content: "c", Project: "proj"},
	}

	report := CheckStaleness(files)

	var found bool
	for _, a := range report.Alerts {
		if a.Type == "index-mismatch" {
			found = true
		}
	}
	if !found {
		t.Error("expected an index-mismatch alert")
	}
}
