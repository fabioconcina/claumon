package memory

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

type MemoryFile struct {
	Path        string `json:"path"`
	Project     string `json:"project"`
	Category    string `json:"category"` // "claude-md", "rules", "auto-memory", "memory-file"
	Content     string `json:"content"`
	HTMLContent string `json:"html_content"`
	ModTime     int64  `json:"mod_time"`

	// Parsed frontmatter fields
	FMName        string `json:"fm_name,omitempty"`
	FMDescription string `json:"fm_description,omitempty"`
	FMType        string `json:"fm_type,omitempty"` // user, feedback, project, reference
}

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithHardWraps()),
)

func RenderMarkdown(source string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		return "<pre>" + source + "</pre>"
	}
	return buf.String()
}

// parseFrontmatter extracts YAML frontmatter fields from markdown content.
// Returns the frontmatter values and the body without the frontmatter block.
func parseFrontmatter(content string) (name, description, fmType, body string) {
	body = content
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return
	}

	// Find the closing ---
	rest := trimmed[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return
	}

	fmBlock := rest[:idx]
	body = strings.TrimSpace(rest[idx+4:])

	// Simple line-by-line YAML parsing (no dependency needed)
	for _, line := range strings.Split(fmBlock, "\n") {
		line = strings.TrimSpace(line)
		if colon := strings.Index(line, ":"); colon > 0 {
			key := strings.TrimSpace(line[:colon])
			val := strings.TrimSpace(line[colon+1:])
			// Strip surrounding quotes
			val = strings.Trim(val, "\"'")
			val = strings.TrimSpace(val)
			switch key {
			case "name":
				name = val
			case "description":
				description = val
			case "type":
				fmType = val
			}
		}
	}
	return
}

func DiscoverAll(claudeDir string) ([]*MemoryFile, error) {
	var files []*MemoryFile

	// 1. Global CLAUDE.md
	if mf := readMemoryFile(filepath.Join(claudeDir, "CLAUDE.md"), "", "claude-md"); mf != nil {
		files = append(files, mf)
	}

	// 2. Global rules
	globalRules := filepath.Join(claudeDir, "rules")
	if entries, err := filepath.Glob(filepath.Join(globalRules, "*.md")); err == nil {
		for _, e := range entries {
			if mf := readMemoryFile(e, "", "rules"); mf != nil {
				files = append(files, mf)
			}
		}
	}

	// 3. Per-project memories
	projectsDir := filepath.Join(claudeDir, "projects")
	projEntries, err := os.ReadDir(projectsDir)
	if err != nil {
		return files, nil
	}

	for _, projEntry := range projEntries {
		if !projEntry.IsDir() {
			continue
		}
		projName := DecodePath(projEntry.Name())
		projDir := filepath.Join(projectsDir, projEntry.Name())

		// Auto-memory: MEMORY.md
		memoryDir := filepath.Join(projDir, "memory")
		if mf := readMemoryFile(filepath.Join(memoryDir, "MEMORY.md"), projName, "auto-memory"); mf != nil {
			files = append(files, mf)
		}

		// Individual memory files in memory/
		if entries, err := os.ReadDir(memoryDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "MEMORY.md" {
					continue
				}
				if mf := readMemoryFile(filepath.Join(memoryDir, e.Name()), projName, "memory-file"); mf != nil {
					files = append(files, mf)
				}
			}
		}

		// Project CLAUDE.md (in actual project directory)
		for _, rel := range []string{"CLAUDE.md", ".claude/CLAUDE.md"} {
			p := filepath.Join(projName, rel)
			if mf := readMemoryFile(p, projName, "claude-md"); mf != nil {
				files = append(files, mf)
				break
			}
		}

		// Project rules
		projRulesDir := filepath.Join(projName, ".claude", "rules")
		if entries, err := filepath.Glob(filepath.Join(projRulesDir, "*.md")); err == nil {
			for _, e := range entries {
				if mf := readMemoryFile(e, projName, "rules"); mf != nil {
					files = append(files, mf)
				}
			}
		}
	}

	return files, nil
}

func readMemoryFile(path, project, category string) *MemoryFile {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	content := string(data)
	fmName, fmDesc, fmType, body := parseFrontmatter(content)

	// Render only the body (without frontmatter) as HTML
	htmlContent := RenderMarkdown(body)

	return &MemoryFile{
		Path:          path,
		Project:       project,
		Category:      category,
		Content:       content,
		HTMLContent:   htmlContent,
		ModTime:       info.ModTime().Unix(),
		FMName:        fmName,
		FMDescription: fmDesc,
		FMType:        fmType,
	}
}

// ReadFile reads and renders a single memory file by path.
func ReadFile(path, project, category string) *MemoryFile {
	return readMemoryFile(path, project, category)
}

// DecodePath converts an encoded project directory name back to a filesystem path.
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

	return "/" + strings.ReplaceAll(encoded, "-", "/")
}

// SearchMemories searches all memory files for a query string (case-insensitive).
func SearchMemories(memories []*MemoryFile, query string) []*MemoryFile {
	if query == "" {
		return memories
	}
	q := strings.ToLower(query)
	var results []*MemoryFile
	for _, m := range memories {
		if strings.Contains(strings.ToLower(m.Content), q) ||
			strings.Contains(strings.ToLower(m.Path), q) ||
			strings.Contains(strings.ToLower(m.Project), q) ||
			strings.Contains(strings.ToLower(m.FMName), q) ||
			strings.Contains(strings.ToLower(m.FMDescription), q) {
			results = append(results, m)
		}
	}
	return results
}

// WatchPaths returns all paths that should be watched for memory file changes.
func WatchPaths(claudeDir string) []string {
	var paths []string

	paths = append(paths, claudeDir)

	if _, err := os.Stat(filepath.Join(claudeDir, "rules")); err == nil {
		paths = append(paths, filepath.Join(claudeDir, "rules"))
	}

	projectsDir := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return paths
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		projDir := filepath.Join(projectsDir, e.Name())
		paths = append(paths, projDir)

		memDir := filepath.Join(projDir, "memory")
		if _, err := os.Stat(memDir); err == nil {
			paths = append(paths, memDir)
		}
	}

	return paths
}

// ModTimeFormatted returns a human-readable modification time.
func (m *MemoryFile) ModTimeFormatted() string {
	return time.Unix(m.ModTime, 0).Format("2006-01-02 15:04:05")
}
