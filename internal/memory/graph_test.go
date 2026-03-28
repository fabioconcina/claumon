package memory

import (
	"testing"
)

func TestBuildGraphEmpty(t *testing.T) {
	data := BuildGraph(nil)
	if len(data.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(data.Nodes))
	}
	if len(data.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(data.Edges))
	}
}

func TestBuildGraphNodes(t *testing.T) {
	files := []*MemoryFile{
		{Path: "/a/CLAUDE.md", Project: "projA", Category: "claude-md"},
		{Path: "/a/mem/note.md", Project: "projA", Category: "memory-file", FMName: "My Note"},
	}

	data := BuildGraph(files)
	if len(data.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(data.Nodes))
	}

	// Node with FMName should use it as label
	if data.Nodes[1].Label != "My Note" {
		t.Errorf("node label = %q, want %q", data.Nodes[1].Label, "My Note")
	}
}

func TestBuildGraphIndexLinks(t *testing.T) {
	files := []*MemoryFile{
		{Path: "/proj/memory/MEMORY.md", Project: "proj", Category: "auto-memory", Content: "- [note](note.md)"},
		{Path: "/proj/memory/note.md", Project: "proj", Category: "memory-file"},
	}

	data := BuildGraph(files)

	var foundLink bool
	for _, e := range data.Edges {
		if e.Type == "index-link" && e.Source == "/proj/memory/MEMORY.md" && e.Target == "/proj/memory/note.md" {
			foundLink = true
		}
	}
	if !foundLink {
		t.Error("expected an index-link edge from MEMORY.md to note.md")
	}
}

func TestBuildGraphGroups(t *testing.T) {
	files := []*MemoryFile{
		{Path: "/a/note.md", Project: "/a", Category: "memory-file"},
		{Path: "/a/other.md", Project: "/a", Category: "memory-file"},
		{Path: "/b/note.md", Project: "/b", Category: "memory-file"},
		{Path: "/global.md", Project: "", Category: "claude-md"},
	}

	data := BuildGraph(files)
	if len(data.Groups) != 3 {
		t.Errorf("expected 3 groups, got %d", len(data.Groups))
	}
}

func TestExtractEntities(t *testing.T) {
	content := "SSH to admin@192.168.1.1 and run ~/.local/bin/mytool"
	entities := extractEntities(content)

	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d: %v", len(entities), entities)
	}

	found := map[string]bool{}
	for _, e := range entities {
		found[e] = true
	}
	if !found["ssh:admin@192.168.1.1"] {
		t.Error("expected ssh:admin@192.168.1.1")
	}
	if !found["bin:mytool"] {
		t.Error("expected bin:mytool")
	}
}
