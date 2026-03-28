package memory

import (
	"path/filepath"
	"regexp"
)

type GraphNode struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Project  string `json:"project"`
	Category string `json:"category"`
	FMType   string `json:"fm_type"`
	ModTime  int64  `json:"mod_time"`
}

type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"` // "index-link"
}

type GraphGroup struct {
	Project string `json:"project"`
	Label   string `json:"label"`
	Count   int    `json:"count"`
}

type GraphData struct {
	Nodes  []GraphNode  `json:"nodes"`
	Edges  []GraphEdge  `json:"edges"`
	Groups []GraphGroup `json:"groups"`
}

var (
	sshRefRegex    = regexp.MustCompile(`\b(\w+@[\d.]+)\b`)
	binPathRegex   = regexp.MustCompile(`~/.local/bin/(\w+)`)
)

// BuildGraph creates a graph representation of memory files and their relationships.
func BuildGraph(files []*MemoryFile) *GraphData {
	data := &GraphData{
		Nodes:  []GraphNode{},
		Edges:  []GraphEdge{},
		Groups: []GraphGroup{},
	}

	if len(files) == 0 {
		return data
	}

	// Build nodes
	for _, f := range files {
		label := filepath.Base(f.Path)
		if f.FMName != "" {
			label = f.FMName
		}
		data.Nodes = append(data.Nodes, GraphNode{
			ID:       f.Path,
			Label:    label,
			Project:  f.Project,
			Category: f.Category,
			FMType:   f.FMType,
			ModTime:  f.ModTime,
		})
	}

	// Index-link edges: MEMORY.md -> referenced files
	knownPaths := pathIndex(files)
	for _, f := range files {
		if f.Category != "auto-memory" {
			continue
		}
		dir := filepath.Dir(f.Path)
		for _, href := range extractMarkdownLinks(f.Content) {
			target := filepath.Join(dir, href)
			if knownPaths[target] {
				data.Edges = append(data.Edges, GraphEdge{
					Source: f.Path,
					Target: target,
					Type:   "index-link",
				})
			}
		}
	}

	// Build groups
	groupCounts := make(map[string]int)
	for _, f := range files {
		groupCounts[f.Project]++
	}
	for proj, count := range groupCounts {
		label := proj
		if label == "" {
			label = "Global"
		} else {
			label = filepath.Base(proj)
		}
		data.Groups = append(data.Groups, GraphGroup{
			Project: proj,
			Label:   label,
			Count:   count,
		})
	}

	return data
}

func extractEntities(content string) []string {
	var entities []string
	for _, m := range sshRefRegex.FindAllString(content, -1) {
		entities = append(entities, "ssh:"+m)
	}
	for _, m := range binPathRegex.FindAllStringSubmatch(content, -1) {
		entities = append(entities, "bin:"+m[1])
	}
	return entities
}
