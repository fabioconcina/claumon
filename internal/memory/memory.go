package memory

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

var localLinkRe = regexp.MustCompile(`href="([^"]*\.md)"`)

// rewriteLocalLinks converts relative .md links in rendered HTML to vscode:// URLs.
func rewriteLocalLinks(html, dir string) string {
	return localLinkRe.ReplaceAllStringFunc(html, func(match string) string {
		sub := localLinkRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		href := sub[1]
		// Skip already-absolute URLs
		if strings.HasPrefix(href, "http") || strings.HasPrefix(href, "vscode") {
			return match
		}
		abs := filepath.Join(dir, href)
		return `href="vscode://file/` + abs + `"`
	})
}

func renderMarkdown(source string) string {
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
	files = append(files, globMarkdownFiles(filepath.Join(claudeDir, "rules"), "", "rules")...)

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
		files = append(files, discoverProjectMemories(projDir, projName)...)
	}

	return files, nil
}

// globMarkdownFiles reads every *.md file in dir as a memory of the given category/project.
// Missing directories return nil, not an error.
func globMarkdownFiles(dir, project, category string) []*MemoryFile {
	entries, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil
	}
	var out []*MemoryFile
	for _, e := range entries {
		if mf := readMemoryFile(e, project, category); mf != nil {
			out = append(out, mf)
		}
	}
	return out
}

// discoverProjectMemories collects all memory sources for a single project:
// auto-memory MEMORY.md, individual memory files, project CLAUDE.md, and project rules.
func discoverProjectMemories(projDir, projName string) []*MemoryFile {
	var out []*MemoryFile

	memoryDir := filepath.Join(projDir, "memory")
	if mf := readMemoryFile(filepath.Join(memoryDir, "MEMORY.md"), projName, "auto-memory"); mf != nil {
		out = append(out, mf)
	}

	if entries, err := os.ReadDir(memoryDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "MEMORY.md" {
				continue
			}
			if mf := readMemoryFile(filepath.Join(memoryDir, e.Name()), projName, "memory-file"); mf != nil {
				out = append(out, mf)
			}
		}
	}

	// Project CLAUDE.md (in actual project directory, first match wins)
	for _, rel := range []string{"CLAUDE.md", ".claude/CLAUDE.md"} {
		if mf := readMemoryFile(filepath.Join(projName, rel), projName, "claude-md"); mf != nil {
			out = append(out, mf)
			break
		}
	}

	out = append(out, globMarkdownFiles(filepath.Join(projName, ".claude", "rules"), projName, "rules")...)
	return out
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
	dir := filepath.Dir(path)
	htmlContent := rewriteLocalLinks(renderMarkdown(body), dir)

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

